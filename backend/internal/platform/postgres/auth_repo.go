package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"askdocs/backend/internal/auth"
)

// AuthRepository implements auth.Repository on Postgres.
type AuthRepository struct {
	pool *pgxpool.Pool
}

func NewAuthRepository(pool *pgxpool.Pool) *AuthRepository {
	return &AuthRepository{pool: pool}
}

func (r *AuthRepository) CreateUser(ctx context.Context, email, passwordHash string) (auth.User, error) {
	var user auth.User
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 RETURNING id, email, created_at`,
		email, passwordHash,
	).Scan(&user.ID, &user.Email, &user.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return auth.User{}, auth.ErrEmailTaken
		}
		return auth.User{}, fmt.Errorf("insert user: %w", err)
	}
	return user, nil
}

func (r *AuthRepository) UserByEmail(ctx context.Context, email string) (auth.User, string, error) {
	var user auth.User
	var hash string
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, created_at, password_hash FROM users WHERE email = $1`, email,
	).Scan(&user.ID, &user.Email, &user.CreatedAt, &hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.User{}, "", auth.ErrUserNotFound
		}
		return auth.User{}, "", fmt.Errorf("select user by email: %w", err)
	}
	return user, hash, nil
}

func (r *AuthRepository) UserByID(ctx context.Context, id string) (auth.User, error) {
	var user auth.User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, created_at FROM users WHERE id = $1`, id,
	).Scan(&user.ID, &user.Email, &user.CreatedAt)
	if err != nil {
		if notFound(err) {
			return auth.User{}, auth.ErrUserNotFound
		}
		return auth.User{}, fmt.Errorf("select user: %w", err)
	}
	return user, nil
}

func (r *AuthRepository) CreateSession(ctx context.Context, tokenHash, userID string, expiresAt time.Time) error {
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		tokenHash, userID, expiresAt); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *AuthRepository) SessionUserID(ctx context.Context, tokenHash string) (string, time.Time, error) {
	var userID string
	var expiresAt time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT user_id, expires_at FROM sessions WHERE token_hash = $1`, tokenHash,
	).Scan(&userID, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", time.Time{}, auth.ErrSessionNotFound
		}
		return "", time.Time{}, fmt.Errorf("select session: %w", err)
	}
	return userID, expiresAt, nil
}

func (r *AuthRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrSessionNotFound
	}
	return nil
}
