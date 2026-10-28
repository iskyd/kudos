package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/store/db"
)

const (
	sessionCookieName = "kudos_session"
	sessionTTL        = 30 * 24 * time.Hour
	// Sliding expiry is written at most once per interval to limit database writes.
	sessionExtendEvery = 24 * time.Hour
	csrfHeader         = "X-CSRF-Token"
)

// publicOperations need no session. Every other operation requires one, as the
// spec's global `security` says; keep this list in step with `security: []`.
func publicOperations() map[string]bool {
	return map[string]bool{"GetHealthz": true, "Register": true, "Login": true}
}

type session struct {
	tokenHash []byte
	userID    int64
	username  string
	timezone  string
	csrf      string
}

type ctxKey int

const (
	sessionKey ctxKey = iota
	secureKey
)

func sessionFrom(ctx context.Context) *session {
	s, _ := ctx.Value(sessionKey).(*session)
	return s
}

func secureFrom(ctx context.Context) bool {
	secure, _ := ctx.Value(secureKey).(bool)
	return secure
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// sessionCookie returns the Set-Cookie value for token; an empty token clears the cookie.
func sessionCookie(token string, expires time.Time, secure bool) string {
	c := &http.Cookie{ //nolint:gosec // G124: Secure is set whenever the request came over HTTPS; plain-HTTP LAN installs cannot use it
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	}
	if token == "" {
		c.MaxAge = -1
		c.Expires = time.Time{}
	}
	return c.String()
}

// isHTTPS trusts X-Forwarded-Proto: it can only make the cookie stricter.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// loadSession resolves the session cookie into the request context and slides
// its expiry. A missing, unknown or expired cookie leaves the request anonymous.
func (s *Server) loadSession(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), secureKey, isHTTPS(r))
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil || cookie.Value == "" {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			hash := hashToken(cookie.Value)
			row, err := s.q.GetSession(ctx, hash)
			if errors.Is(err, pgx.ErrNoRows) {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if err != nil {
				writeProblem(w, r, logger, err)
				return
			}

			if expires := time.Now().Add(sessionTTL); expires.Sub(row.ExpiresAt) >= sessionExtendEvery {
				if err := s.q.ExtendSession(ctx, db.ExtendSessionParams{TokenHash: hash, UserID: row.UserID, ExpiresAt: expires}); err != nil {
					writeProblem(w, r, logger, err)
					return
				}
				w.Header().Add("Set-Cookie", sessionCookie(cookie.Value, expires, isHTTPS(r)))
			}

			ctx = context.WithValue(ctx, sessionKey, &session{
				tokenHash: hash,
				userID:    row.UserID,
				username:  row.Username,
				timezone:  row.Timezone,
				csrf:      row.CsrfToken,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requireSession rejects non-public operations without a session, and
// state-changing ones without the session's CSRF token.
func requireSession(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
	if publicOperations()[operationID] {
		return f
	}
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		sess := sessionFrom(ctx)
		if sess == nil {
			return nil, &Error{Kind: KindUnauthorized}
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead &&
			subtle.ConstantTimeCompare([]byte(r.Header.Get(csrfHeader)), []byte(sess.csrf)) != 1 {
			return nil, &Error{Kind: KindCSRF}
		}
		return f(ctx, w, r, request)
	}
}

// startSession stores a new session for userID and returns its cookie and CSRF token.
func (s *Server) startSession(ctx context.Context, userID int64) (cookie, csrf string, err error) {
	token, csrf := rand.Text(), rand.Text()
	expires := time.Now().Add(sessionTTL)
	if err := s.q.CreateSession(ctx, db.CreateSessionParams{
		TokenHash: hashToken(token),
		UserID:    userID,
		CsrfToken: csrf,
		ExpiresAt: expires,
	}); err != nil {
		return "", "", fmt.Errorf("creating session: %w", err)
	}
	return sessionCookie(token, expires, secureFrom(ctx)), csrf, nil
}
