// Package auth is the authentication domain: users, credentials and opaque
// session tokens. Decision (recorded): sessions are opaque random tokens
// delivered in an httpOnly cookie and stored server-side by hash — no JWT,
// nothing decodable client-side, instant revocation on logout.
package auth

import (
	"context"
	"errors"
	"time"
)

var (
	ErrEmailTaken         = errors.New("email is already registered")
	ErrInvalidEmail       = errors.New("email must look like an email address")
	ErrWeakPassword       = errors.New("password must have at least 8 characters")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrSessionInvalid     = errors.New("session is invalid or expired")
	ErrUserNotFound       = errors.New("user not found")
	ErrSessionNotFound    = errors.New("session not found")
)

type User struct {
	ID        string
	Email     string
	CreatedAt time.Time
}

// Repository persists users and sessions. Implemented by platform/postgres.
type Repository interface {
	CreateUser(ctx context.Context, email, passwordHash string) (User, error)
	UserByEmail(ctx context.Context, email string) (User, string, error) // user, password hash
	UserByID(ctx context.Context, id string) (User, error)
	CreateSession(ctx context.Context, tokenHash, userID string, expiresAt time.Time) error
	SessionUserID(ctx context.Context, tokenHash string) (string, time.Time, error) // userID, expiresAt
	DeleteSession(ctx context.Context, tokenHash string) error
}
