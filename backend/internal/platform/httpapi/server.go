// Package httpapi is the primary adapter: it exposes the application over HTTP.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Pinger reports whether the backing database is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// New assembles the HTTP handler tree for the API.
func New(logger *slog.Logger, db Pinger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", handleHealthz(db))
	return withRequestLog(logger, mux)
}

func handleHealthz(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")
		if err := db.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"unavailable","database":"unreachable"}`))
			return
		}
		w.Write([]byte(`{"status":"ok"}`))
	}
}
