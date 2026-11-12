package store_test

import (
	"errors"
	"reflect"
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

// TestGarminSyncAndBackfillIsolation runs every sync, backfill and import
// write as bob, on rows keyed like alice's, and checks hers are untouched.
func TestGarminSyncAndBackfillIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	for _, u := range []int64{alice.ID, bob.ID} {
		_, err := q.UpsertGarminKey(ctx, garminKey(u, "key"))
		check(t, err)
		_, err = q.CreateGarminBackfill(ctx, db.CreateGarminBackfillParams{UserID: u, WindowStart: day(1), WindowEnd: day(20)})
		check(t, err)
		for _, id := range []string{"1", "2"} {
			check(t, q.AddGarminActivityImport(ctx, db.AddGarminActivityImportParams{UserID: u, SourceActivityID: id, StartTime: start}))
		}
	}
	type state struct {
		Conn    db.GarminConnection
		Backf   db.GarminBackfill
		Counts  db.CountGarminActivityImportsRow
		Next    db.GarminActivityImport
		Failure []db.ListGarminActivityImportFailuresRow
	}
	read := func(u int64) state {
		t.Helper()
		var s state
		var err error
		s.Conn, err = q.GetGarminConnection(ctx, u)
		check(t, err)
		s.Backf, err = q.GetGarminBackfill(ctx, u)
		check(t, err)
		s.Counts, err = q.CountGarminActivityImports(ctx, u)
		check(t, err)
		if s.Next, err = q.NextGarminActivityImport(ctx, u); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		s.Failure, err = q.ListGarminActivityImportFailures(ctx, db.ListGarminActivityImportFailuresParams{UserID: u, MaxRows: 10})
		check(t, err)
		return s
	}
	before := read(alice.ID)

	exp := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	reason, msg := "rate_limited", "boom"
	check(t, q.UpdateGarminToken(ctx, db.UpdateGarminTokenParams{UserID: bob.ID, TokenCiphertext: []byte("bob-token"), TokenExpiresAt: &exp}))
	check(t, q.StartGarminSyncAttempt(ctx, bob.ID))
	check(t, q.SetGarminSynced(ctx, db.SetGarminSyncedParams{UserID: bob.ID, SyncedThrough: &exp}))
	check(t, q.SetGarminSyncError(ctx, db.SetGarminSyncErrorParams{UserID: bob.ID, LastError: &msg}))
	check(t, q.SetGarminBackfillListed(ctx, bob.ID))
	check(t, q.SetGarminBackfillHealthThrough(ctx, db.SetGarminBackfillHealthThroughParams{UserID: bob.ID, HealthThrough: &exp}))
	check(t, q.SetGarminActivityImportDone(ctx, db.SetGarminActivityImportDoneParams{UserID: bob.ID, SourceActivityID: "1"}))
	check(t, q.SetGarminActivityImportFailed(ctx, db.SetGarminActivityImportFailedParams{UserID: bob.ID, SourceActivityID: "2", Error: &msg}))
	if n, err := q.FinishGarminBackfill(ctx, bob.ID); err != nil || n != 1 {
		t.Errorf("bob finishes %d backfills, %v; want his 1", n, err)
	}
	if n, err := q.RetryGarminActivityImports(ctx, bob.ID); err != nil || n != 1 {
		t.Errorf("bob retries %d imports, %v; want his 1", n, err)
	}
	_, err := q.RunGarminBackfill(ctx, bob.ID)
	check(t, err)
	check(t, q.SetGarminActivityImportDone(ctx, db.SetGarminActivityImportDoneParams{UserID: bob.ID, SourceActivityID: "2"}))
	check(t, q.PauseGarminBackfill(ctx, db.PauseGarminBackfillParams{UserID: bob.ID, PauseReason: &reason}))
	check(t, q.SetGarminKeyRejected(ctx, bob.ID))

	if after := read(alice.ID); !reflect.DeepEqual(after, before) {
		t.Errorf("alice's sync state changed:\n got %+v\nwant %+v", after, before)
	}
	b := read(bob.ID)
	if b.Conn.Status != "key_rejected" || string(b.Conn.TokenCiphertext) != "bob-token" || b.Backf.Status != "paused" ||
		b.Counts.Done != 2 || b.Counts.Total != 2 || len(b.Failure) != 0 {
		t.Errorf("bob's writes did not reach his rows: %+v", b)
	}
}
