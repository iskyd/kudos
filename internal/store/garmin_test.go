package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func garminKey(userID int64, key string) db.UpsertGarminKeyParams {
	exp := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	return db.UpsertGarminKeyParams{
		UserID: userID, KeyCiphertext: []byte(key), KeyExpiresOn: day(28),
		TokenCiphertext: []byte("token-" + key), TokenExpiresAt: &exp,
	}
}

func TestGarminConnectionRoundTrip(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	c, err := q.UpsertGarminKey(ctx, garminKey(alice.ID, "k1"))
	check(t, err)
	if c.Status != "connected" || c.SyncIntervalH != 1 || c.SyncedThrough != nil || c.LastSyncAt != nil ||
		!c.KeyExpiresOn.Equal(day(28)) || string(c.TokenCiphertext) != "token-k1" {
		t.Errorf("new connection = %+v", c)
	}

	// A rejected key and a custom interval: a new key clears the rejection and keeps the interval.
	_, err = pool.Exec(ctx, "UPDATE garmin_connections SET status = 'key_rejected' WHERE user_id = $1", alice.ID)
	check(t, err)
	_, err = q.SetGarminSyncInterval(ctx, db.SetGarminSyncIntervalParams{UserID: alice.ID, SyncIntervalH: 6})
	check(t, err)
	c, err = q.UpsertGarminKey(ctx, garminKey(alice.ID, "k2"))
	check(t, err)
	if c.Status != "connected" || c.SyncIntervalH != 6 || string(c.KeyCiphertext) != "k2" {
		t.Errorf("after a new key = %+v", c)
	}

	got, err := q.GetGarminConnection(ctx, alice.ID)
	check(t, err)
	if string(got.KeyCiphertext) != "k2" {
		t.Errorf("get = %+v", got)
	}

	n, err := q.DeleteGarminConnection(ctx, alice.ID)
	if err != nil || n != 1 {
		t.Fatalf("delete = %d, %v", n, err)
	}
	if _, err := q.GetGarminConnection(ctx, alice.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("after delete: %v", err)
	}
}

func TestGarminConnectionChecks(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	_, err := q.UpsertGarminKey(t.Context(), garminKey(alice.ID, "k"))
	check(t, err)

	for _, h := range []int16{0, 25} {
		var pgErr *pgconn.PgError
		if _, err := q.SetGarminSyncInterval(t.Context(), db.SetGarminSyncIntervalParams{UserID: alice.ID, SyncIntervalH: h}); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("interval %d: error = %v, want a check violation", h, err)
		}
	}
	// A token needs its expiry.
	p := garminKey(alice.ID, "k")
	p.TokenExpiresAt = nil
	var pgErr *pgconn.PgError
	if _, err := q.UpsertGarminKey(t.Context(), p); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("token without expiry: error = %v, want a check violation", err)
	}
}

func TestGarminConnectionIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	_, err := q.UpsertGarminKey(ctx, garminKey(alice.ID, "alice-key"))
	check(t, err)

	if _, err := q.GetGarminConnection(ctx, bob.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("bob reads a connection: %v", err)
	}
	if _, err := q.SetGarminSyncInterval(ctx, db.SetGarminSyncIntervalParams{UserID: bob.ID, SyncIntervalH: 9}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("bob sets an interval: %v", err)
	}
	if n, err := q.DeleteGarminConnection(ctx, bob.ID); err != nil || n != 0 {
		t.Errorf("bob deletes %d rows, %v", n, err)
	}
	// Bob's own key is his own row.
	_, err = q.UpsertGarminKey(ctx, garminKey(bob.ID, "bob-key"))
	check(t, err)

	a, err := q.GetGarminConnection(ctx, alice.ID)
	check(t, err)
	if string(a.KeyCiphertext) != "alice-key" || a.SyncIntervalH != 1 {
		t.Errorf("alice's connection changed: %+v", a)
	}
}
