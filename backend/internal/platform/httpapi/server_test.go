package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"askdocs/backend/internal/document"
	"askdocs/backend/internal/query"
)

type pingFunc func(ctx context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

func newTestServer(db Pinger) http.Handler {
	docs := document.NewService(newMemRepo(), &memStore{})
	queries := query.NewService(newMemQueryRepo(), stubEmbedder{}, stubVectorStore{}, &stubLLM{})
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)), db, docs, queries)
}

func TestHealthzOK(t *testing.T) {
	srv := newTestServer(pingFunc(func(context.Context) error { return nil }))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("body = %s, want status ok", rec.Body.String())
	}
}

func TestHealthzDatabaseDown(t *testing.T) {
	srv := newTestServer(pingFunc(func(context.Context) error { return errors.New("boom") }))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(rec.Body.String(), `"database":"unreachable"`) {
		t.Errorf("body = %s, want database unreachable", rec.Body.String())
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	srv := newTestServer(pingFunc(func(context.Context) error { return nil }))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
