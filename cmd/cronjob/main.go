package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "github.com/lib/pq"
)

const whisperAPI = "https://api.openai.com/v1/audio/transcriptions"

type pendingMemory struct {
	ID        string
	AudioPath string
}

func main() {
	dbURL := mustEnv("DATABASE_URL")
	audioDir := mustEnv("AUDIO_DIR")
	apiKey := mustEnv("OPENAI_API_KEY")

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := waitForDB(db); err != nil {
		log.Fatalf("db not ready: %v", err)
	}

	pending, err := claimPending(db)
	if err != nil {
		log.Fatalf("claim pending: %v", err)
	}
	log.Printf("processing %d pending memories", len(pending))

	for _, m := range pending {
		fullPath := filepath.Join(audioDir, m.AudioPath)
		transcript, err := transcribe(apiKey, fullPath)
		if err != nil {
			log.Printf("transcribe %s: %v", m.ID, err)
			if _, dbErr := db.Exec(
				`UPDATE memories SET status='failed', updated_at=now() WHERE id=$1`, m.ID,
			); dbErr != nil {
				log.Printf("mark failed %s: %v", m.ID, dbErr)
			}
			continue
		}
		if _, dbErr := db.Exec(
			`UPDATE memories SET status='done', transcript=$1, updated_at=now() WHERE id=$2`,
			transcript, m.ID,
		); dbErr != nil {
			log.Printf("mark done %s: %v", m.ID, dbErr)
		}
		log.Printf("done %s", m.ID)
	}
}

func claimPending(db *sql.DB) ([]pendingMemory, error) {
	rows, err := db.Query(`
		UPDATE memories SET status='processing', updated_at=now()
		WHERE id IN (
			SELECT id FROM memories WHERE status='pending' FOR UPDATE SKIP LOCKED
		)
		RETURNING id, audio_path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []pendingMemory
	for rows.Next() {
		var m pendingMemory
		if err := rows.Scan(&m.ID, &m.AudioPath); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func waitForDB(db *sql.DB) error {
	for i := 0; i < 10; i++ {
		if db.Ping() == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out")
}

func transcribe(apiKey, audioPath string) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("open audio: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", err
	}
	w.WriteField("model", "whisper-1")
	w.WriteField("language", "pl")
	w.Close()

	req, err := http.NewRequest(http.MethodPost, whisperAPI, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api status %d: %s", resp.StatusCode, data)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	return result.Text, nil
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("env %s is required", key)
	}
	return v
}
