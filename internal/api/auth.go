package api

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/iskyd/kudos/internal/crypto"
	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/store/db"
)

// Keep in step with the Username and Password schemas in api/openapi.yaml.
var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

const (
	minPasswordLen    = 8
	maxPasswordLen    = 256
	pgUniqueViolation = "23505"
)

// Server implements the generated StrictServerInterface.
type Server struct {
	q                   *db.Queries
	ingest              *ingest.Service
	garmin              GarminAuthenticator
	cipher              *crypto.Cipher
	registrationEnabled bool
	argon               argonParams
	// dummyHash is verified against when a username is unknown, so login takes
	// the same time whether or not the account exists.
	dummyHash string
}

var _ StrictServerInterface = (*Server)(nil)

// NewServer returns the API server backed by q, storing activities with ingester.
func NewServer(q *db.Queries, ingester *ingest.Service, garmin GarminAuthenticator, cipher *crypto.Cipher,
	registrationEnabled bool,
) *Server {
	return newServer(q, ingester, garmin, cipher, registrationEnabled, defaultArgon())
}

func newServer(q *db.Queries, ingester *ingest.Service, garmin GarminAuthenticator, cipher *crypto.Cipher,
	registrationEnabled bool, p argonParams,
) *Server {
	return &Server{
		q:                   q,
		ingest:              ingester,
		garmin:              garmin,
		cipher:              cipher,
		registrationEnabled: registrationEnabled,
		argon:               p,
		dummyHash:           hashPassword(p, "dummy password"),
	}
}

// GetHealthz is the liveness probe.
func (*Server) GetHealthz(context.Context, GetHealthzRequestObject) (GetHealthzResponseObject, error) {
	return GetHealthz200TextResponse("ok"), nil
}

// Register creates an account and logs it in.
func (s *Server) Register(ctx context.Context, req RegisterRequestObject) (RegisterResponseObject, error) {
	if !s.registrationEnabled {
		return nil, &Error{Kind: KindRegistrationDisabled}
	}
	username := strings.ToLower(req.Body.Username)
	if !usernamePattern.MatchString(username) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: "username must be 3 to 32 characters among a-z, 0-9, '.', '_' and '-'"}
	}
	if n := utf8.RuneCountInString(req.Body.Password); n < minPasswordLen || n > maxPasswordLen {
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("password must be %d to %d characters", minPasswordLen, maxPasswordLen)}
	}
	tz := "UTC"
	if req.Body.Timezone != nil {
		tz = *req.Body.Timezone
	}
	if err := validateTimezone(tz); err != nil {
		return nil, err
	}

	user, err := s.q.CreateUser(ctx, db.CreateUserParams{
		Username:     username,
		PasswordHash: hashPassword(s.argon, req.Body.Password),
		Timezone:     tz,
	})
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return nil, &Error{Kind: KindUsernameTaken}
	}
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	cookie, csrf, err := s.startSession(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return Register201JSONResponse{
		Body:    Me{Id: user.ID, Username: user.Username, Timezone: user.Timezone, CsrfToken: csrf},
		Headers: Register201ResponseHeaders{SetCookie: &cookie},
	}, nil
}

// Login checks the credentials and starts a session.
func (s *Server) Login(ctx context.Context, req LoginRequestObject) (LoginResponseObject, error) {
	user, err := s.q.GetUserCredentials(ctx, strings.ToLower(req.Body.Username))
	hash := user.PasswordHash
	if errors.Is(err, pgx.ErrNoRows) {
		hash = s.dummyHash
	} else if err != nil {
		return nil, fmt.Errorf("loading credentials: %w", err)
	}
	ok, err := verifyPassword(hash, req.Body.Password)
	if err != nil {
		return nil, fmt.Errorf("verifying password: %w", err)
	}
	if !ok || hash == s.dummyHash {
		return nil, &Error{Kind: KindInvalidCredentials}
	}

	if err := s.q.DeleteExpiredSessions(ctx, user.ID); err != nil {
		return nil, fmt.Errorf("deleting expired sessions: %w", err)
	}
	cookie, csrf, err := s.startSession(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return Login200JSONResponse{
		Body:    Me{Id: user.ID, Username: user.Username, Timezone: user.Timezone, CsrfToken: csrf},
		Headers: Login200ResponseHeaders{SetCookie: &cookie},
	}, nil
}

// Logout ends the current session.
func (s *Server) Logout(ctx context.Context, _ LogoutRequestObject) (LogoutResponseObject, error) {
	sess := sessionFrom(ctx)
	if err := s.q.DeleteSession(ctx, db.DeleteSessionParams{TokenHash: sess.tokenHash, UserID: sess.userID}); err != nil {
		return nil, fmt.Errorf("deleting session: %w", err)
	}
	cookie := sessionCookie("", time.Time{}, secureFrom(ctx))
	return Logout204Response{Headers: Logout204ResponseHeaders{SetCookie: &cookie}}, nil
}

// GetMe returns the current user and the session's CSRF token.
func (*Server) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	sess := sessionFrom(ctx)
	return GetMe200JSONResponse{Id: sess.userID, Username: sess.username, Timezone: sess.timezone, CsrfToken: sess.csrf}, nil
}

// UpdateMe changes the current user's timezone.
func (s *Server) UpdateMe(ctx context.Context, req UpdateMeRequestObject) (UpdateMeResponseObject, error) {
	if err := validateTimezone(req.Body.Timezone); err != nil {
		return nil, err
	}
	sess := sessionFrom(ctx)
	user, err := s.q.UpdateUserTimezone(ctx, db.UpdateUserTimezoneParams{ID: sess.userID, Timezone: req.Body.Timezone})
	if err != nil {
		return nil, fmt.Errorf("updating timezone: %w", err)
	}
	return UpdateMe200JSONResponse{Id: user.ID, Username: user.Username, Timezone: user.Timezone, CsrfToken: sess.csrf}, nil
}

// validateTimezone accepts IANA zone names only; "Local" would mean the server's zone.
func validateTimezone(tz string) error {
	if _, err := time.LoadLocation(tz); err != nil || tz == "" || tz == "Local" {
		return &Error{Kind: KindInvalidRequest, Detail: "timezone must be an IANA time zone name, e.g. Europe/Rome"}
	}
	return nil
}
