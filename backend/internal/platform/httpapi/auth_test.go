package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"askdocs/backend/internal/auth"
)

// memAuthRepo is an in-memory auth.Repository so handler tests run the real
// auth service (bcrypt, tokens, expiry) without Postgres.
type memAuthRepo struct {
	users    map[string]auth.User
	byEmail  map[string]string
	hashes   map[string]string
	sessions map[string]memSession
	nextID   int
}

type memSession struct {
	userID    string
	expiresAt time.Time
}

func newMemAuthRepo() *memAuthRepo {
	return &memAuthRepo{
		users:    map[string]auth.User{},
		byEmail:  map[string]string{},
		hashes:   map[string]string{},
		sessions: map[string]memSession{},
	}
}

func (r *memAuthRepo) CreateUser(_ context.Context, email, passwordHash string) (auth.User, error) {
	if _, taken := r.byEmail[email]; taken {
		return auth.User{}, auth.ErrEmailTaken
	}
	r.nextID++
	user := auth.User{ID: fmt.Sprintf("user-%d", r.nextID), Email: email}
	r.users[user.ID] = user
	r.byEmail[email] = user.ID
	r.hashes[user.ID] = passwordHash
	return user, nil
}

func (r *memAuthRepo) UserByEmail(_ context.Context, email string) (auth.User, string, error) {
	id, ok := r.byEmail[email]
	if !ok {
		return auth.User{}, "", auth.ErrUserNotFound
	}
	return r.users[id], r.hashes[id], nil
}

func (r *memAuthRepo) UserByID(_ context.Context, id string) (auth.User, error) {
	user, ok := r.users[id]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return user, nil
}

func (r *memAuthRepo) CreateSession(_ context.Context, tokenHash, userID string, expiresAt time.Time) error {
	r.sessions[tokenHash] = memSession{userID, expiresAt}
	return nil
}

func (r *memAuthRepo) SessionUserID(_ context.Context, tokenHash string) (string, time.Time, error) {
	s, ok := r.sessions[tokenHash]
	if !ok {
		return "", time.Time{}, auth.ErrSessionNotFound
	}
	return s.userID, s.expiresAt, nil
}

func (r *memAuthRepo) DeleteSession(_ context.Context, tokenHash string) error {
	if _, ok := r.sessions[tokenHash]; !ok {
		return auth.ErrSessionNotFound
	}
	delete(r.sessions, tokenHash)
	return nil
}

func TestRegisterLoginLogoutFlow(t *testing.T) {
	env := okEnv(t)
	cookie := env.register("alice@example.com")

	// Session works.
	rec := env.do(http.MethodGet, "/auth/me", nil, "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var me userResponse
	json.Unmarshal(rec.Body.Bytes(), &me)
	if me.Email != "alice@example.com" {
		t.Errorf("me = %+v, want alice@example.com", me)
	}

	// Logout kills the session.
	rec = env.do(http.MethodPost, "/auth/logout", nil, "", cookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d", rec.Code)
	}
	rec = env.do(http.MethodGet, "/auth/me", nil, "", cookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: status = %d, want 401", rec.Code)
	}

	// Login issues a fresh session.
	rec = env.do(http.MethodPost, "/auth/login",
		strings.NewReader(`{"email":"alice@example.com","password":"supersecret"}`), "application/json", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var fresh *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			fresh = c
		}
	}
	if fresh == nil {
		t.Fatal("login did not set a session cookie")
	}
	if rec := env.do(http.MethodGet, "/auth/me", nil, "", fresh); rec.Code != http.StatusOK {
		t.Fatalf("me after login: status = %d", rec.Code)
	}
}

func TestRegisterDuplicateEmailIs409(t *testing.T) {
	env := okEnv(t)
	env.register("alice@example.com")

	rec := env.do(http.MethodPost, "/auth/register",
		strings.NewReader(`{"email":"alice@example.com","password":"othersecret"}`), "application/json", nil)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestRegisterWeakPasswordIs400(t *testing.T) {
	env := okEnv(t)

	rec := env.do(http.MethodPost, "/auth/register",
		strings.NewReader(`{"email":"a@b.com","password":"short"}`), "application/json", nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestLoginWrongPasswordIs401(t *testing.T) {
	env := okEnv(t)
	env.register("alice@example.com")

	rec := env.do(http.MethodPost, "/auth/login",
		strings.NewReader(`{"email":"alice@example.com","password":"wrongwrong"}`), "application/json", nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestProtectedRoutesRequireSession(t *testing.T) {
	env := okEnv(t)

	protected := []struct{ method, path string }{
		{http.MethodGet, "/documents"},
		{http.MethodPost, "/documents"},
		{http.MethodGet, "/documents/x"},
		{http.MethodPost, "/documents/x/retry"},
		{http.MethodPost, "/queries"},
		{http.MethodGet, "/conversations/x"},
		{http.MethodGet, "/auth/me"},
	}
	for _, route := range protected {
		rec := env.do(route.method, route.path, strings.NewReader("{}"), "application/json", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without session: status = %d, want 401", route.method, route.path, rec.Code)
		}
	}
}
