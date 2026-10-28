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

func TestCreateUserRejectsDuplicateUsername(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	testutil.TwoUsers(t, pool)

	_, err := db.New(pool).CreateUser(t.Context(), db.CreateUserParams{Username: "alice", PasswordHash: "x", Timezone: "UTC"})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("CreateUser duplicate error = %v, want unique violation", err)
	}
}

func TestGetUserCredentials(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)

	got, err := q.GetUserCredentials(t.Context(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != alice.ID || got.Username != "alice" || got.PasswordHash != "unusable" || got.Timezone != "UTC" {
		t.Fatalf("GetUserCredentials = %+v", got)
	}
	if _, err := q.GetUserCredentials(t.Context(), "nobody"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unknown user error = %v, want ErrNoRows", err)
	}
}

func TestUpdateUserTimezoneIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)

	got, err := q.UpdateUserTimezone(t.Context(), db.UpdateUserTimezoneParams{ID: alice.ID, Timezone: "Europe/Rome"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != alice.ID || got.Timezone != "Europe/Rome" {
		t.Fatalf("UpdateUserTimezone = %+v", got)
	}
	b, err := q.GetUserCredentials(t.Context(), bob.Username)
	if err != nil {
		t.Fatal(err)
	}
	if b.Timezone != "UTC" {
		t.Fatalf("bob's timezone changed to %q", b.Timezone)
	}
}

func TestSessionsIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	future := time.Now().Add(time.Hour)

	create := func(token string, userID int64, expires time.Time) {
		t.Helper()
		if err := q.CreateSession(ctx, db.CreateSessionParams{TokenHash: []byte(token), UserID: userID, CsrfToken: "csrf-" + token, ExpiresAt: expires}); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(token string) bool {
		t.Helper()
		_, err := q.GetSession(ctx, []byte(token))
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		return err == nil
	}
	create("alice", alice.ID, future)
	create("bob", bob.ID, future)
	create("bob-expired", bob.ID, time.Now().Add(-time.Hour))

	got, err := q.GetSession(ctx, []byte("alice"))
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != alice.ID || got.Username != "alice" || got.CsrfToken != "csrf-alice" {
		t.Fatalf("GetSession = %+v", got)
	}
	if exists("bob-expired") {
		t.Fatal("expired session returned")
	}

	// Alice cannot extend or delete Bob's session through her user ID.
	later := time.Now().Add(48 * time.Hour)
	if err := q.ExtendSession(ctx, db.ExtendSessionParams{TokenHash: []byte("bob"), UserID: alice.ID, ExpiresAt: later}); err != nil {
		t.Fatal(err)
	}
	if b, _ := q.GetSession(ctx, []byte("bob")); b.ExpiresAt.After(future.Add(time.Minute)) {
		t.Fatal("alice extended bob's session")
	}
	if err := q.DeleteSession(ctx, db.DeleteSessionParams{TokenHash: []byte("bob"), UserID: alice.ID}); err != nil {
		t.Fatal(err)
	}
	if !exists("bob") {
		t.Fatal("alice deleted bob's session")
	}
	if err := q.DeleteExpiredSessions(ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	// ExtendSession ignores expiry, so reviving a row shows whether it still exists.
	revived := func(token string) bool {
		t.Helper()
		if err := q.ExtendSession(ctx, db.ExtendSessionParams{TokenHash: []byte(token), UserID: bob.ID, ExpiresAt: future}); err != nil {
			t.Fatal(err)
		}
		return exists(token)
	}
	if !revived("bob-expired") {
		t.Fatal("alice's cleanup deleted bob's expired session")
	}
	if err := q.ExtendSession(ctx, db.ExtendSessionParams{TokenHash: []byte("bob-expired"), UserID: bob.ID, ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	// Owners can.
	if err := q.ExtendSession(ctx, db.ExtendSessionParams{TokenHash: []byte("bob"), UserID: bob.ID, ExpiresAt: later}); err != nil {
		t.Fatal(err)
	}
	if b, _ := q.GetSession(ctx, []byte("bob")); !b.ExpiresAt.After(future.Add(time.Minute)) {
		t.Fatal("bob could not extend his session")
	}
	if err := q.DeleteExpiredSessions(ctx, bob.ID); err != nil {
		t.Fatal(err)
	}
	if revived("bob-expired") {
		t.Fatal("bob's expired session survived cleanup")
	}
	if err := q.DeleteSession(ctx, db.DeleteSessionParams{TokenHash: []byte("bob"), UserID: bob.ID}); err != nil {
		t.Fatal(err)
	}
	if exists("bob") {
		t.Fatal("bob could not delete his session")
	}
}
