// Package config loads the API process settings from environment variables.
package config

import "os"

// Config holds every environment-driven setting for the API process.
// Defaults match the local docker compose setup.
type Config struct {
	APIPort      string
	DatabaseURL  string
	AIServiceURL string
	UploadDir    string
}

func Load() Config {
	return Config{
		APIPort:      getenv("API_PORT", "8080"),
		DatabaseURL:  getenv("DATABASE_URL", "postgres://askdocs:askdocs@localhost:5433/askdocs?sslmode=disable"),
		AIServiceURL: getenv("AI_SERVICE_URL", "http://localhost:8000"),
		UploadDir:    getenv("UPLOAD_DIR", "./data/uploads"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
