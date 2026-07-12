package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimitKicksInAfterBurst(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")

	var last int
	limited := false
	// The burst is 30 and register/login already spent a few tokens; well
	// within 100 requests the limiter must start answering 429.
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/documents", nil)
		req.RemoteAddr = "203.0.113.7:1234"
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		env.srv.ServeHTTP(rec, req)
		last = rec.Code
		if rec.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatalf("no 429 after 100 rapid requests (last status %d)", last)
	}
}

func TestRateLimitIsPerIP(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("a@example.com")

	// Exhaust one IP.
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/documents", nil)
		req.RemoteAddr = "203.0.113.8:1234"
		req.AddCookie(cookie)
		env.srv.ServeHTTP(httptest.NewRecorder(), req)
	}

	// A different IP still gets through.
	req := httptest.NewRequest(http.MethodGet, "/documents", nil)
	req.RemoteAddr = "198.51.100.9:1234"
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	env.srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fresh IP: status = %d, want 200", rec.Code)
	}
}

func TestHealthzExemptFromRateLimit(t *testing.T) {
	env := okEnv(t)

	for i := 0; i < 200; i++ {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "203.0.113.9:1234"
		rec := httptest.NewRecorder()
		env.srv.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("healthz rate-limited on request %d", i)
		}
	}
}
