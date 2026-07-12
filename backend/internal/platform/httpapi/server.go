// Package httpapi is the primary adapter: it exposes the application over HTTP.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"askdocs/backend/internal/document"
)

// Pinger reports whether the backing database is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

type api struct {
	logger *slog.Logger
	db     Pinger
	docs   *document.Service
}

// New assembles the HTTP handler tree for the API.
func New(logger *slog.Logger, db Pinger, docs *document.Service) http.Handler {
	a := &api{logger: logger, db: db, docs: docs}

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", a.handleHealthz())
	mux.Handle("POST /documents", a.handleUploadDocument())
	mux.Handle("GET /documents", a.handleListDocuments())
	mux.Handle("GET /documents/{id}", a.handleGetDocument())
	mux.Handle("POST /documents/{id}/retry", a.handleRetryDocument())
	return withRequestLog(logger, mux)
}

func (a *api) handleHealthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := a.db.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status":   "unavailable",
				"database": "unreachable",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
