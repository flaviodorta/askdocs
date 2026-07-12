// Package config loads the API process settings from environment variables.
package config

import (
	"os"
	"strconv"
)

// Config holds every environment-driven setting for the API process.
// Defaults match the local docker compose setup.
type Config struct {
	APIPort       string
	DatabaseURL   string
	AIServiceURL  string
	UploadDir     string
	IngestWorkers int
}

func Load() Config {
	return Config{
		APIPort:       getenv("API_PORT", "8080"),
		DatabaseURL:   getenv("DATABASE_URL", "postgres://askdocs:askdocs@localhost:5433/askdocs?sslmode=disable"),
		AIServiceURL:  getenv("AI_SERVICE_URL", "http://localhost:8000"),
		UploadDir:     getenv("UPLOAD_DIR", "./data/uploads"),
		IngestWorkers: getenvInt("INGEST_WORKERS", 2),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil && n > 0 {
		return n
	}
	return fallback
}
