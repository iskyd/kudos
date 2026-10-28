package testutil

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/iskyd/kudos/internal/store/db"
)

// TwoUsers creates users "alice" and "bob" for cross-user isolation tests.
// Their password hashes are placeholders: they cannot log in.
func TwoUsers(t testing.TB, pool *pgxpool.Pool) (alice, bob db.CreateUserRow) {
	t.Helper()
	q := db.New(pool)
	create := func(name string) db.CreateUserRow {
		u, err := q.CreateUser(t.Context(), db.CreateUserParams{Username: name, PasswordHash: "unusable", Timezone: "UTC"})
		if err != nil {
			t.Fatalf("creating user %s: %v", name, err)
		}
		return u
	}
	return create("alice"), create("bob")
}
