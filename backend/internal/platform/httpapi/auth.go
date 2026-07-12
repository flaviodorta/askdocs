package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"askdocs/backend/internal/auth"
)

const sessionCookie = "askdocs_session"

type contextKey struct{}

var userIDKey contextKey

// userID returns the authenticated user set by requireAuth. Handlers behind
// the middleware can rely on it being non-empty.
func userID(r *http.Request) string {
	id, _ := r.Context().Value(userIDKey).(string)
	return id
}

// requireAuth resolves the session cookie to a user and injects the user id
// into the request context. Everything except /healthz and /auth/* runs
// behind it.
func (a *api) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		uid, err := a.auth.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, auth.ErrSessionInvalid) {
				writeError(w, http.StatusUnauthorized, "session expired — log in again")
				return
			}
			a.logger.Error("authenticate", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userIDKey, uid)))
	}
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func decodeCredentials(w http.ResponseWriter, r *http.Request) (credentialsRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var req credentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return credentialsRequest{}, false
	}
	return req, true
}

// setSessionCookie: httpOnly keeps the token away from JS; SameSite=Lax
// blocks cross-site POSTs. Secure is off because local dev runs over http —
// turn it on behind TLS.
func setSessionCookie(w http.ResponseWriter, token string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *api) handleRegister() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeCredentials(w, r)
		if !ok {
			return
		}

		user, token, err := a.auth.Register(r.Context(), req.Email, req.Password)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrEmailTaken):
				writeError(w, http.StatusConflict, err.Error())
			case errors.Is(err, auth.ErrInvalidEmail), errors.Is(err, auth.ErrWeakPassword):
				writeError(w, http.StatusBadRequest, err.Error())
			default:
				a.logger.Error("register", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}

		setSessionCookie(w, token, auth.SessionTTL)
		writeJSON(w, http.StatusCreated, userResponse{ID: user.ID, Email: user.Email})
	}
}

func (a *api) handleLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeCredentials(w, r)
		if !ok {
			return
		}

		user, token, err := a.auth.Login(r.Context(), req.Email, req.Password)
		if err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) {
				writeError(w, http.StatusUnauthorized, err.Error())
				return
			}
			a.logger.Error("login", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		setSessionCookie(w, token, auth.SessionTTL)
		writeJSON(w, http.StatusOK, userResponse{ID: user.ID, Email: user.Email})
	}
}

func (a *api) handleLogout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			if err := a.auth.Logout(r.Context(), cookie.Value); err != nil {
				a.logger.Error("logout", "error", err)
			}
		}
		setSessionCookie(w, "", -time.Second) // MaxAge < 0 deletes the cookie
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *api) handleMe() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.auth.User(r.Context(), userID(r))
		if err != nil {
			a.logger.Error("me", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, userResponse{ID: user.ID, Email: user.Email})
	}
}
