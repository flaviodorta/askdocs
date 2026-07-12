package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// SessionTTL is how long a login lasts. Sessions are not sliding — after the
// TTL the user logs in again.
const SessionTTL = 7 * 24 * time.Hour

const minPasswordLen = 8

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Register creates the account and logs it in (returns a session token).
func (s *Service) Register(ctx context.Context, email, password string) (User, string, error) {
	email = normalizeEmail(email)
	if !looksLikeEmail(email) {
		return User{}, "", ErrInvalidEmail
	}
	if len(password) < minPasswordLen {
		return User{}, "", ErrWeakPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, "", fmt.Errorf("hash password: %w", err)
	}

	user, err := s.repo.CreateUser(ctx, email, string(hash))
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			return User{}, "", ErrEmailTaken
		}
		return User{}, "", fmt.Errorf("create user: %w", err)
	}

	token, err := s.startSession(ctx, user.ID)
	if err != nil {
		return User{}, "", err
	}
	return user, token, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (User, string, error) {
	user, hash, err := s.repo.UserByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// Same cost and same error as a wrong password, so responses
			// don't reveal which emails are registered.
			bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidinval"), []byte(password))
			return User{}, "", ErrInvalidCredentials
		}
		return User{}, "", fmt.Errorf("look up user: %w", err)
	}

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return User{}, "", ErrInvalidCredentials
	}

	token, err := s.startSession(ctx, user.ID)
	if err != nil {
		return User{}, "", err
	}
	return user, token, nil
}

// Authenticate resolves a session token to the owning user id.
func (s *Service) Authenticate(ctx context.Context, token string) (string, error) {
	userID, expiresAt, err := s.repo.SessionUserID(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return "", ErrSessionInvalid
		}
		return "", fmt.Errorf("look up session: %w", err)
	}
	if time.Now().After(expiresAt) {
		// Lazy cleanup: expired sessions are removed when they show up.
		_ = s.repo.DeleteSession(ctx, hashToken(token))
		return "", ErrSessionInvalid
	}
	return userID, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	err := s.repo.DeleteSession(ctx, hashToken(token))
	if err != nil && !errors.Is(err, ErrSessionNotFound) {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Service) User(ctx context.Context, id string) (User, error) {
	return s.repo.UserByID(ctx, id)
}

func (s *Service) startSession(ctx context.Context, userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	token := hex.EncodeToString(raw)

	if err := s.repo.CreateSession(ctx, hashToken(token), userID, time.Now().Add(SessionTTL)); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func looksLikeEmail(email string) bool {
	at := strings.Index(email, "@")
	return at > 0 && at < len(email)-1 && !strings.ContainsAny(email, " \t\n")
}
