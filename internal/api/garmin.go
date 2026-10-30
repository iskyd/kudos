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
	"github.com/iskyd/kudos/internal/connectors/garminsync"
	"github.com/iskyd/kudos/internal/crypto"
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

// GarminDeps holds what the Garmin endpoints need.
type GarminDeps struct {
	Auth   GarminAuthenticator
	Cipher *crypto.Cipher      // seals the key and token
	Sync   *garminsync.Service // backfill and ongoing sync
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
	today := localToday(sess.timezone)
	expires := req.Body.ExpiresOn.Time
	// Garmin keys last at most a year; a day of slack covers timezones.
	if !expires.After(today) || expires.After(today.AddDate(1, 0, 1)) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: "expiresOn must be after today and at most a year ahead"}
	}

	gs, err := s.garmin.Auth.Authenticate(ctx, connectors.Credentials{Key: secret.New(key)})
	switch {
	case errors.Is(err, connectors.ErrKeyRejected):
		return nil, &Error{Kind: KindGarminKeyRejected, Detail: "check the key and that it has not expired or been revoked"}
	case err != nil:
		// The connector's errors name the status, never the key.
		return nil, &Error{Kind: KindGarminUnavailable, Detail: "Garmin could not check the key; try again later"}
	}
	keyCT, err := s.garmin.Cipher.Seal([]byte(key))
	if err != nil {
		return nil, fmt.Errorf("encrypting the Garmin key: %w", err)
	}
	tokenCT, err := s.garmin.Cipher.Seal([]byte(gs.Token.Reveal()))
	if err != nil {
		return nil, fmt.Errorf("encrypting the Garmin token: %w", err)
	}
	c, err := s.q.UpsertGarminKey(ctx, db.UpsertGarminKeyParams{
		UserID: sess.userID, KeyCiphertext: keyCT, KeyExpiresOn: expires, TokenCiphertext: tokenCT, TokenExpiresAt: &gs.Expires,
	})
	if err != nil {
		return nil, fmt.Errorf("storing the Garmin key: %w", err)
	}
	// A backfill paused by the old key continues with the new one.
	if err := s.garmin.Sync.Resume(ctx, sess.userID, false); err != nil && !errors.Is(err, garminsync.ErrNotFound) {
		return nil, fmt.Errorf("resuming the backfill: %w", err)
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
		SyncIntervalHours: &hours, LastSyncAt: c.LastSyncAt, LastError: c.LastError,
	}
	if c.SyncedThrough != nil {
		out.SyncedThrough = &openapi_types.Date{Time: *c.SyncedThrough}
	}
	return out
}

// localToday is the user's current calendar day, at midnight UTC like stored dates.
func localToday(tz string) time.Time {
	y, m, d := time.Now().In(location(tz)).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// SyncGarmin queues a sync now, unless one is already queued or running.
func (s *Server) SyncGarmin(ctx context.Context, _ SyncGarminRequestObject) (SyncGarminResponseObject, error) {
	queued, err := s.garmin.Sync.SyncNow(ctx, sessionFrom(ctx).userID)
	switch {
	case errors.Is(err, garminsync.ErrNotConnected):
		return nil, &Error{Kind: KindNotFound, Detail: "Garmin is not connected"}
	case errors.Is(err, garminsync.ErrBackfillRunning):
		return nil, &Error{Kind: KindBackfillRunning, Detail: "sync starts when the history import finishes"}
	case errors.Is(err, connectors.ErrKeyRejected):
		return nil, &Error{Kind: KindGarminKeyRejected, Detail: "enter a new key to sync again"}
	case err != nil:
		return nil, fmt.Errorf("queueing a sync: %w", err)
	}
	return SyncGarmin202JSONResponse{Queued: queued}, nil
}
