package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"memories/static"
)

type Memory struct {
	ID         string    `json:"id"`
	Transcript *string   `json:"transcript"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

var (
	db         *sql.DB
	audioDir string
)

func main() {
	dbURL := mustEnv("DATABASE_URL")
	audioDir = mustEnv("AUDIO_DIR")
	port := envOr("PORT", "8080")

	if err := os.MkdirAll(audioDir, 0755); err != nil {
		log.Fatalf("create audio dir: %v", err)
	}

	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/api/upload", handleUpload)
	mux.HandleFunc("/api/memories", handleMemories)
	mux.HandleFunc("/", handleIndex)

	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("env %s is required", key)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(static.IndexHTML)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "audio field required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	id := newUUID()
	filename := id + guessExt(header.Header.Get("Content-Type"))
	dest, err := os.Create(filepath.Join(audioDir, filename))
	if err != nil {
		log.Printf("create file: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer dest.Close()

	if _, err := io.Copy(dest, file); err != nil {
		log.Printf("write file: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var m Memory
	err = db.QueryRow(
		`INSERT INTO memories (id, audio_path) VALUES ($1, $2)
		 RETURNING id, transcript, status, created_at, updated_at`,
		id, filename,
	).Scan(&m.ID, &m.Transcript, &m.Status, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		log.Printf("insert memory: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(m)
}

func handleMemories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rows, err := db.Query(
		`SELECT id, transcript, status, created_at, updated_at
		 FROM memories ORDER BY created_at DESC`,
	)
	if err != nil {
		log.Printf("query memories: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	memories := []Memory{}
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Transcript, &m.Status, &m.CreatedAt, &m.UpdatedAt); err != nil {
			log.Printf("scan memory: %v", err)
			continue
		}
		memories = append(memories, m)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(memories)
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func guessExt(ct string) string {
	ct = strings.TrimSpace(strings.ToLower(strings.SplitN(ct, ";", 2)[0]))
	switch ct {
	case "audio/webm", "video/webm":
		return ".webm"
	case "audio/ogg":
		return ".ogg"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return ".m4a"
	case "audio/wav", "audio/wave":
		return ".wav"
	default:
		return ".audio"
	}
}