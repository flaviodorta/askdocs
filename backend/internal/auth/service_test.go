package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type fakeRepo struct {
	users    map[string]User   // by id
	byEmail  map[string]string // email → id
	hashes   map[string]string // id → password hash
	sessions map[string]struct {
		userID    string
		expiresAt time.Time
	}
	nextID int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		users:   map[string]User{},
		byEmail: map[string]string{},
		hashes:  map[string]string{},
		sessions: map[string]struct {
			userID    string
			expiresAt time.Time
		}{},
	}
}

func (r *fakeRepo) CreateUser(_ context.Context, email, passwordHash string) (User, error) {
	if _, taken := r.byEmail[email]; taken {
		return User{}, ErrEmailTaken
	}
	r.nextID++
	user := User{ID: fmt.Sprintf("user-%d", r.nextID), Email: email}
	r.users[user.ID] = user
	r.byEmail[email] = user.ID
	r.hashes[user.ID] = passwordHash
	return user, nil
}

func (r *fakeRepo) UserByEmail(_ context.Context, email string) (User, string, error) {
	id, ok := r.byEmail[email]
	if !ok {
		return User{}, "", ErrUserNotFound
	}
	return r.users[id], r.hashes[id], nil
}

func (r *fakeRepo) UserByID(_ context.Context, id string) (User, error) {
	user, ok := r.users[id]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return user, nil
}

func (r *fakeRepo) CreateSession(_ context.Context, tokenHash, userID string, expiresAt time.Time) error {
	r.sessions[tokenHash] = struct {
		userID    string
		expiresAt time.Time
	}{userID, expiresAt}
	return nil
}

func (r *fakeRepo) SessionUserID(_ context.Context, tokenHash string) (string, time.Time, error) {
	s, ok := r.sessions[tokenHash]
	if !ok {
		return "", time.Time{}, ErrSessionNotFound
	}
	return s.userID, s.expiresAt, nil
}

func (r *fakeRepo) DeleteSession(_ context.Context, tokenHash string) error {
	if _, ok := r.sessions[tokenHash]; !ok {
		return ErrSessionNotFound
	}
	delete(r.sessions, tokenHash)
	return nil
}

func TestRegisterAndAuthenticate(t *testing.T) {
	svc := NewService(newFakeRepo())

	user, token, err := svc.Register(context.Background(), " Alice@Example.com ", "supersecret")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("email = %q, want normalized lowercase", user.Email)
	}
	if token == "" {
		t.Fatal("no session token issued on register")
	}

	userID, err := svc.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if userID != user.ID {
		t.Errorf("authenticated user = %q, want %q", userID, user.ID)
	}
}

func TestRegisterValidation(t *testing.T) {
	svc := NewService(newFakeRepo())

	if _, _, err := svc.Register(context.Background(), "not-an-email", "supersecret"); !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("bad email: error = %v, want ErrInvalidEmail", err)
	}
	if _, _, err := svc.Register(context.Background(), "a@b.com", "short"); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("short password: error = %v, want ErrWeakPassword", err)
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	svc := NewService(newFakeRepo())
	svc.Register(context.Background(), "a@b.com", "supersecret")

	if _, _, err := svc.Register(context.Background(), "A@b.com", "othersecret"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate: error = %v, want ErrEmailTaken", err)
	}
}

func TestLogin(t *testing.T) {
	svc := NewService(newFakeRepo())
	svc.Register(context.Background(), "a@b.com", "supersecret")

	if _, _, err := svc.Login(context.Background(), "a@b.com", "wrongpassword"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: error = %v, want ErrInvalidCredentials", err)
	}
	if _, _, err := svc.Login(context.Background(), "ghost@b.com", "supersecret"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown email: error = %v, want ErrInvalidCredentials (no user enumeration)", err)
	}

	user, token, err := svc.Login(context.Background(), "a@b.com", "supersecret")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if user.Email != "a@b.com" || token == "" {
		t.Errorf("login = %+v token=%q", user, token)
	}
}

func TestAuthenticateRejectsExpiredSession(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	_, token, _ := svc.Register(context.Background(), "a@b.com", "supersecret")

	// Age the session past its TTL.
	hash := hashToken(token)
	s := repo.sessions[hash]
	s.expiresAt = time.Now().Add(-time.Minute)
	repo.sessions[hash] = s

	if _, err := svc.Authenticate(context.Background(), token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("expired session: error = %v, want ErrSessionInvalid", err)
	}
	if _, ok := repo.sessions[hash]; ok {
		t.Error("expired session was not lazily deleted")
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	svc := NewService(newFakeRepo())
	_, token, _ := svc.Register(context.Background(), "a@b.com", "supersecret")

	if err := svc.Logout(context.Background(), token); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := svc.Authenticate(context.Background(), token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("after logout: error = %v, want ErrSessionInvalid", err)
	}
	// Logging out twice is fine.
	if err := svc.Logout(context.Background(), token); err != nil {
		t.Fatalf("second Logout() error = %v", err)
	}
}
