package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/iskyd/kudos/internal/connectors"
	"github.com/iskyd/kudos/internal/secret"
	"github.com/iskyd/kudos/internal/store/db"
)

// Keep in step with api/openapi.yaml.
const (
	maxGarminKeyLen = 1024
	maxSyncInterval = 24
)

// GarminAuthenticator checks a Personal API key by exchanging it for a token;
// the Garmin connector in production.
type GarminAuthenticator interface {
	Authenticate(ctx context.Context, creds connectors.Credentials) (*connectors.Session, error)
}

// GetGarminConnection returns the connection state, never the key or token.
func (s *Server) GetGarminConnection(ctx context.Context, _ GetGarminConnectionRequestObject) (GetGarminConnectionResponseObject, error) {
	c, err := s.q.GetGarminConnection(ctx, sessionFrom(ctx).userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GetGarminConnection200JSONResponse{Connected: false}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the Garmin connection: %w", err)
	}
	return GetGarminConnection200JSONResponse(garminConnection(c)), nil
}

// SetGarminKey checks the key with Garmin and stores it and its token encrypted.
func (s *Server) SetGarminKey(ctx context.Context, req SetGarminKeyRequestObject) (SetGarminKeyResponseObject, error) {
	sess := sessionFrom(ctx)
	key := strings.TrimSpace(req.Body.Key)
	if key == "" || len(key) > maxGarminKeyLen {
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("key must be 1 to %d characters", maxGarminKeyLen)}
	}
	y, m, d := time.Now().In(location(sess.timezone)).Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	expires := req.Body.ExpiresOn.Time
	// Garmin keys last at most a year; a day of slack covers timezones.
	if !expires.After(today) || expires.After(today.AddDate(1, 0, 1)) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: "expiresOn must be after today and at most a year ahead"}
	}

	gs, err := s.garmin.Authenticate(ctx, connectors.Credentials{Key: secret.New(key)})
	switch {
	case errors.Is(err, connectors.ErrKeyRejected):
		return nil, &Error{Kind: KindGarminKeyRejected, Detail: "check the key and that it has not expired or been revoked"}
	case err != nil:
		// The connector's errors name the status, never the key.
		return nil, &Error{Kind: KindGarminUnavailable, Detail: "Garmin could not check the key; try again later"}
	}
	keyCT, err := s.cipher.Seal([]byte(key))
	if err != nil {
		return nil, fmt.Errorf("encrypting the Garmin key: %w", err)
	}
	tokenCT, err := s.cipher.Seal([]byte(gs.Token.Reveal()))
	if err != nil {
		return nil, fmt.Errorf("encrypting the Garmin token: %w", err)
	}
	c, err := s.q.UpsertGarminKey(ctx, db.UpsertGarminKeyParams{
		UserID: sess.userID, KeyCiphertext: keyCT, KeyExpiresOn: expires, TokenCiphertext: tokenCT, TokenExpiresAt: &gs.Expires,
	})
	if err != nil {
		return nil, fmt.Errorf("storing the Garmin key: %w", err)
	}
	return SetGarminKey200JSONResponse(garminConnection(c)), nil
}

// SetGarminSyncInterval sets how often Garmin is synced, in hours.
func (s *Server) SetGarminSyncInterval(ctx context.Context, req SetGarminSyncIntervalRequestObject) (SetGarminSyncIntervalResponseObject, error) {
	h := req.Body.Hours
	if h < 1 || h > maxSyncInterval {
		return nil, &Error{Kind: KindInvalidRequest, Detail: "hours must be 1 to 24"}
	}
	c, err := s.q.SetGarminSyncInterval(ctx, db.SetGarminSyncIntervalParams{UserID: sessionFrom(ctx).userID, SyncIntervalH: int16(h)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &Error{Kind: KindNotFound, Detail: "Garmin is not connected"}
	}
	if err != nil {
		return nil, fmt.Errorf("setting the Garmin sync interval: %w", err)
	}
	return SetGarminSyncInterval200JSONResponse(garminConnection(c)), nil
}

// DeleteGarminConnection deletes the key and token; synced data is kept.
func (s *Server) DeleteGarminConnection(ctx context.Context, _ DeleteGarminConnectionRequestObject) (DeleteGarminConnectionResponseObject, error) {
	if _, err := s.q.DeleteGarminConnection(ctx, sessionFrom(ctx).userID); err != nil {
		return nil, fmt.Errorf("deleting the Garmin connection: %w", err)
	}
	return DeleteGarminConnection204Response{}, nil
}

// garminConnection maps the stored row to the API; it has no field for the key or token.
func garminConnection(c db.GarminConnection) GarminConnection {
	status := GarminConnectionStatus(c.Status)
	hours := int(c.SyncIntervalH)
	out := GarminConnection{
		Connected: true, Status: &status, KeyExpiresOn: &openapi_types.Date{Time: c.KeyExpiresOn},
		SyncIntervalHours: &hours, LastSyncAt: c.LastSyncAt,
	}
	if c.SyncedThrough != nil {
		out.SyncedThrough = &openapi_types.Date{Time: *c.SyncedThrough}
	}
	return out
}
