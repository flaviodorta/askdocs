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

	"askdocs/backend/internal/auth"
	"askdocs/backend/internal/document"
	"askdocs/backend/internal/query"
)

type pingFunc func(ctx context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

// testEnv wires the full handler tree over in-memory adapters and a real
// auth service, so every test crosses the same middleware as production.
type testEnv struct {
	t       *testing.T
	srv     http.Handler
	docRepo *memRepo
	vector  *stubVectorStore
	llm     *stubLLM
}

func newTestEnv(t *testing.T, db Pinger) *testEnv {
	t.Helper()
	docRepo := newMemRepo()
	vector := &stubVectorStore{}
	llm := &stubLLM{}
	docs := document.NewService(docRepo, &memStore{})
	queries := query.NewService(newMemQueryRepo(), stubEmbedder{}, vector, llm)
	authSvc := auth.NewService(newMemAuthRepo())
	srv := New(slog.New(slog.NewTextHandler(io.Discard, nil)), db, docs, queries, authSvc)
	return &testEnv{t: t, srv: srv, docRepo: docRepo, vector: vector, llm: llm}
}

func okEnv(t *testing.T) *testEnv {
	return newTestEnv(t, pingFunc(func(context.Context) error { return nil }))
}

func (e *testEnv) do(method, path string, body io.Reader, contentType string, cookie *http.Cookie) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

// register creates an account and returns its session cookie.
func (e *testEnv) register(email string) *http.Cookie {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/auth/register",
		strings.NewReader(`{"email":"`+email+`","password":"supersecret"}`), "application/json", nil)
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("register %s: status = %d, body = %s", email, rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	e.t.Fatal("register did not set a session cookie")
	return nil
}

func TestHealthzOKAndPublic(t *testing.T) {
	env := okEnv(t)

	rec := env.do(http.MethodGet, "/healthz", nil, "", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("body = %s, want status ok", rec.Body.String())
	}
}

func TestHealthzDatabaseDown(t *testing.T) {
	env := newTestEnv(t, pingFunc(func(context.Context) error { return errors.New("boom") }))

	rec := env.do(http.MethodGet, "/healthz", nil, "", nil)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	env := okEnv(t)

	rec := env.do(http.MethodGet, "/nope", nil, "", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
