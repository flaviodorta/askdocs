package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{"API_PORT", "DATABASE_URL", "AI_SERVICE_URL", "UPLOAD_DIR"} {
		t.Setenv(key, "")
	}

	cfg := Load()

	if cfg.APIPort != "8080" {
		t.Errorf("APIPort = %q, want 8080", cfg.APIPort)
	}
	if cfg.UploadDir != "./data/uploads" {
		t.Errorf("UploadDir = %q, want ./data/uploads", cfg.UploadDir)
	}
	if cfg.DatabaseURL != "postgres://askdocs:askdocs@localhost:5433/askdocs?sslmode=disable" {
		t.Errorf("DatabaseURL = %q, want local default", cfg.DatabaseURL)
	}
	if cfg.AIServiceURL != "http://localhost:8000" {
		t.Errorf("AIServiceURL = %q, want http://localhost:8000", cfg.AIServiceURL)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("API_PORT", "9999")
	t.Setenv("DATABASE_URL", "postgres://other:other@db:5432/other")
	t.Setenv("AI_SERVICE_URL", "http://ai:8001")

	cfg := Load()

	if cfg.APIPort != "9999" {
		t.Errorf("APIPort = %q, want 9999", cfg.APIPort)
	}
	if cfg.DatabaseURL != "postgres://other:other@db:5432/other" {
		t.Errorf("DatabaseURL = %q, want env value", cfg.DatabaseURL)
	}
	if cfg.AIServiceURL != "http://ai:8001" {
		t.Errorf("AIServiceURL = %q, want env value", cfg.AIServiceURL)
	}
}
