package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "github.com/lib/pq"
)

const whisperBase = "http://localhost:9000"

type pendingMemory struct {
	ID        string
	AudioPath string
}

func main() {
	dbURL := mustEnv("DATABASE_URL")
	audioDir := mustEnv("AUDIO_DIR")

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := waitForDB(db); err != nil {
		log.Fatalf("db not ready: %v", err)
	}

	if err := waitForWhisper(); err != nil {
		log.Fatalf("whisper not ready: %v", err)
	}

	pending, err := claimPending(db)
	if err != nil {
		log.Fatalf("claim pending: %v", err)
	}
	log.Printf("processing %d pending memories", len(pending))

	for _, m := range pending {
		fullPath := filepath.Join(audioDir, m.AudioPath)
		transcript, err := transcribe(fullPath)
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

	shutdownWhisper()
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

func waitForWhisper() error {
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 60; i++ {
		resp, err := client.Get(whisperBase + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		log.Printf("waiting for whisper (%d/60)…", i+1)
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timed out")
}

func transcribe(audioPath string) (string, error) {
	body, _ := json.Marshal(map[string]string{"audio_path": audioPath})
	resp, err := http.Post(whisperBase+"/transcribe", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, data)
	}
	var result struct {
		Transcript string `json:"transcript"`
		Error      string `json:"error"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("%s", result.Error)
	}
	return result.Transcript, nil
}

func shutdownWhisper() {
	resp, err := http.Post(whisperBase+"/shutdown", "application/json", nil)
	if err != nil {
		log.Printf("shutdown whisper: %v", err)
		return
	}
	resp.Body.Close()
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("env %s is required", key)
	}
	return v
}