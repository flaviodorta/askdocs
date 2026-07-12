// Package httpapi is the primary adapter: it exposes the application over HTTP.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"askdocs/backend/internal/auth"
	"askdocs/backend/internal/document"
	"askdocs/backend/internal/query"
)

// Pinger reports whether the backing database is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

type api struct {
	logger  *slog.Logger
	db      Pinger
	docs    *document.Service
	queries *query.Service
	auth    *auth.Service
}

// New assembles the HTTP handler tree for the API. Everything except
// /healthz and the auth endpoints requires a valid session.
func New(logger *slog.Logger, db Pinger, docs *document.Service, queries *query.Service, authSvc *auth.Service) http.Handler {
	a := &api{logger: logger, db: db, docs: docs, queries: queries, auth: authSvc}

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", a.handleHealthz())

	mux.Handle("POST /auth/register", a.handleRegister())
	mux.Handle("POST /auth/login", a.handleLogin())
	mux.Handle("POST /auth/logout", a.handleLogout())
	mux.Handle("GET /auth/me", a.requireAuth(a.handleMe()))

	mux.Handle("POST /documents", a.requireAuth(a.handleUploadDocument()))
	mux.Handle("GET /documents", a.requireAuth(a.handleListDocuments()))
	mux.Handle("GET /documents/{id}", a.requireAuth(a.handleGetDocument()))
	mux.Handle("POST /documents/{id}/retry", a.requireAuth(a.handleRetryDocument()))
	mux.Handle("POST /queries", a.requireAuth(a.handleAsk()))
	mux.Handle("GET /conversations/{id}", a.requireAuth(a.handleGetConversation()))
	return withRequestLog(logger, withRateLimit(newIPRateLimiter(), mux))
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
