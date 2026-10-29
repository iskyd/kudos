package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/iskyd/kudos/internal/connectors"
	"github.com/iskyd/kudos/internal/secret"
	"github.com/iskyd/kudos/internal/store/db"
)

const (
	validKey  = "valid-personal-api-key-0123456789"
	fakeToken = "fake-garmin-token-abcdef"
)

// fakeGarmin accepts validKey, rejects "rejected-key" and is down for "garmin-down".
type fakeGarmin struct{}

func (fakeGarmin) Authenticate(_ context.Context, c connectors.Credentials) (*connectors.Session, error) {
	switch c.Key.Reveal() {
	case "rejected-key":
		return nil, fmt.Errorf("minting a token: %w", connectors.ErrKeyRejected)
	case "garmin-down":
		return nil, fmt.Errorf("minting a token: %w: status 503", connectors.ErrUpstream)
	}
	return &connectors.Session{Credentials: c, Token: secret.New(fakeToken), Expires: time.Now().Add(24 * time.Hour)}, nil
}

// daysAhead is a date relative to today in UTC, the test user's timezone.
func daysAhead(n int) openapi_types.Date {
	y, m, d := time.Now().UTC().Date()
	return openapi_types.Date{Time: time.Date(y, m, d+n, 0, 0, 0, 0, time.UTC)}
}

// noSecrets fails if a response body holds the key or the token.
func noSecrets(t *testing.T, body string) {
	t.Helper()
	if strings.Contains(body, validKey) || strings.Contains(body, fakeToken) {
		t.Errorf("response leaks a secret: %s", body)
	}
}

func TestGarminConnection(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "UTC")

	rec := c.do(http.MethodGet, "/api/garmin", nil, nil)
	if got := decodeJSON[GarminConnection](t, rec, http.StatusOK); got.Connected {
		t.Fatalf("new user is connected: %+v", got)
	}

	expires := daysAhead(90)
	rec = c.do(http.MethodPut, "/api/garmin/key", SetGarminKeyRequest{Key: "  " + validKey + "\n", ExpiresOn: expires}, nil)
	noSecrets(t, rec.Body.String())
	got := decodeJSON[GarminConnection](t, rec, http.StatusOK)
	if !got.Connected || *got.Status != Connected || !got.KeyExpiresOn.Equal(expires.Time) || *got.SyncIntervalHours != 1 ||
		got.LastSyncAt != nil || got.SyncedThrough != nil {
		t.Errorf("connection = %+v", got)
	}

	// Stored encrypted: the ciphertext does not hold the key, and decrypts to it (trimmed).
	row, err := q.GetGarminConnection(t.Context(), me.Id)
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]struct {
		ct   []byte
		want string
	}{"key": {row.KeyCiphertext, validKey}, "token": {row.TokenCiphertext, fakeToken}} {
		if bytes.Contains(v.ct, []byte(v.want)) {
			t.Errorf("%s stored in plain text", name)
		}
		if plain, err := testCipher(t).Open(v.ct); err != nil || string(plain) != v.want {
			t.Errorf("%s does not decrypt to the original: %v", name, err)
		}
	}
	if row.TokenExpiresAt == nil || time.Until(*row.TokenExpiresAt) < 23*time.Hour {
		t.Errorf("token expiry = %v", row.TokenExpiresAt)
	}

	rec = c.do(http.MethodGet, "/api/garmin", nil, nil)
	noSecrets(t, rec.Body.String())
	if got := decodeJSON[GarminConnection](t, rec, http.StatusOK); !got.Connected {
		t.Errorf("connection = %+v", got)
	}

	rec = c.do(http.MethodPut, "/api/garmin/sync-interval", SetGarminSyncIntervalRequest{Hours: 24}, nil)
	noSecrets(t, rec.Body.String())
	if got := decodeJSON[GarminConnection](t, rec, http.StatusOK); *got.SyncIntervalHours != 24 {
		t.Errorf("interval = %d", *got.SyncIntervalHours)
	}

	// A new key keeps the interval.
	rec = c.do(http.MethodPut, "/api/garmin/key", SetGarminKeyRequest{Key: validKey, ExpiresOn: daysAhead(30)}, nil)
	if got := decodeJSON[GarminConnection](t, rec, http.StatusOK); *got.SyncIntervalHours != 24 || !got.KeyExpiresOn.Equal(daysAhead(30).Time) {
		t.Errorf("after a new key: %+v", got)
	}

	for range 2 { // deleting is idempotent
		if rec := c.do(http.MethodDelete, "/api/garmin", nil, nil); rec.Code != http.StatusNoContent {
			t.Fatalf("delete status = %d: %s", rec.Code, rec.Body)
		}
	}
	if got := decodeJSON[GarminConnection](t, c.do(http.MethodGet, "/api/garmin", nil, nil), http.StatusOK); got.Connected {
		t.Errorf("still connected after delete: %+v", got)
	}
	wantProblem(t, c.do(http.MethodPut, "/api/garmin/sync-interval", SetGarminSyncIntervalRequest{Hours: 2}, nil), KindNotFound)
}

func TestSetGarminKeyRejects(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	c, q := newAPI(t, true, &logs)
	me := c.session(c.do(http.MethodPost, "/api/auth/register", register("alice"), nil), http.StatusCreated)

	tests := []struct {
		name string
		req  SetGarminKeyRequest
		want Kind
	}{
		{"empty key", SetGarminKeyRequest{Key: "  ", ExpiresOn: daysAhead(30)}, KindInvalidRequest},
		{"key too long", SetGarminKeyRequest{Key: strings.Repeat("k", maxGarminKeyLen+1), ExpiresOn: daysAhead(30)}, KindInvalidRequest},
		{"expires today", SetGarminKeyRequest{Key: validKey, ExpiresOn: daysAhead(0)}, KindInvalidRequest},
		{"expires past", SetGarminKeyRequest{Key: validKey, ExpiresOn: daysAhead(-3)}, KindInvalidRequest},
		{"expires beyond a year", SetGarminKeyRequest{Key: validKey, ExpiresOn: daysAhead(400)}, KindInvalidRequest},
		{"rejected by Garmin", SetGarminKeyRequest{Key: "rejected-key", ExpiresOn: daysAhead(30)}, KindGarminKeyRejected},
		{"Garmin down", SetGarminKeyRequest{Key: "garmin-down", ExpiresOn: daysAhead(30)}, KindGarminUnavailable},
	}
	for _, tt := range tests {
		rec := c.do(http.MethodPut, "/api/garmin/key", tt.req, nil)
		if strings.Contains(rec.Body.String(), tt.req.Key) && strings.TrimSpace(tt.req.Key) != "" {
			t.Errorf("%s: the problem echoes the key", tt.name)
		}
		wantProblem(t, rec, tt.want)
	}
	if _, err := q.GetGarminConnection(t.Context(), me.Id); err == nil {
		t.Error("a refused key was stored")
	}
	for _, secret := range []string{"rejected-key", "garmin-down", validKey} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("logs contain the key %q", secret)
		}
	}

	for _, h := range []int{0, 25} {
		wantProblem(t, c.do(http.MethodPut, "/api/garmin/sync-interval", SetGarminSyncIntervalRequest{Hours: h}, nil), KindInvalidRequest)
	}
}

func TestGarminConnectionIsolation(t *testing.T) {
	t.Parallel()
	alice, q, _ := loggedIn(t, "UTC")
	decodeJSON[GarminConnection](t, alice.do(http.MethodPut, "/api/garmin/key", SetGarminKeyRequest{Key: validKey, ExpiresOn: daysAhead(30)}, nil), http.StatusOK)

	bob := &apiClient{t: t, h: alice.h}
	bobMe := bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	if got := decodeJSON[GarminConnection](t, bob.do(http.MethodGet, "/api/garmin", nil, nil), http.StatusOK); got.Connected {
		t.Errorf("bob sees alice's connection: %+v", got)
	}
	wantProblem(t, bob.do(http.MethodPut, "/api/garmin/sync-interval", SetGarminSyncIntervalRequest{Hours: 5}, nil), KindNotFound)
	if rec := bob.do(http.MethodDelete, "/api/garmin", nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
	got := decodeJSON[GarminConnection](t, alice.do(http.MethodGet, "/api/garmin", nil, nil), http.StatusOK)
	if !got.Connected || *got.SyncIntervalHours != 1 {
		t.Errorf("bob changed alice's connection: %+v", got)
	}
	if _, err := q.GetGarminConnection(t.Context(), bobMe.Id); err == nil {
		t.Error("bob has a connection")
	}
}

func TestGarminConnectionMapping(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 10, 29, 6, 0, 0, 0, time.UTC)
	got := garminConnection(db.GarminConnection{
		Status: "key_rejected", KeyExpiresOn: day, SyncIntervalH: 3, SyncedThrough: &day, LastSyncAt: &at,
		KeyCiphertext: []byte("ct"), TokenCiphertext: []byte("ct"),
	})
	if *got.Status != KeyRejected || !got.SyncedThrough.Equal(day) || !got.LastSyncAt.Equal(at) || *got.SyncIntervalHours != 3 {
		t.Errorf("mapping = %+v", got)
	}
}
