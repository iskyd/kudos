package store_test

import (
	"reflect"
	"testing"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

// TestStreamsCompress checks the streams' columnstore policy and that compressed
// chunks read back unchanged and still take an activity's re-import.
func TestStreamsCompress(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool := pg.NewDB(t)
	q := db.New(pool)

	var after string
	if err := pool.QueryRow(ctx, `SELECT config->>'compress_after' FROM timescaledb_information.jobs
		WHERE hypertable_name = 'activity_streams' AND proc_name = 'policy_compression'`).Scan(&after); err != nil {
		t.Fatalf("compression policy: %v", err)
	}
	if after != "30 days" {
		t.Errorf("compress after %q, want 30 days", after)
	}

	alice, _ := testutil.TwoUsers(t, pool)
	id := importActivity(t, q, alice.ID, "compressed", nil)
	before := snapshot(t, q, alice.ID, id)

	// What the policy does to chunks older than 30 days, done now.
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(compress_chunk(c, if_not_compressed => true))
		FROM show_chunks('activity_streams') c`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("compressed %d chunks: %v", n, err)
	}
	if got := snapshot(t, q, alice.ID, id); !reflect.DeepEqual(got.Streams, before.Streams) {
		t.Errorf("compressed streams differ:\n got %+v\nwant %+v", got.Streams, before.Streams)
	}

	// Reprocessing an old activity deletes and re-inserts into compressed chunks.
	importActivity(t, q, alice.ID, "compressed", nil)
	if got := snapshot(t, q, alice.ID, id); !reflect.DeepEqual(got.Streams, before.Streams) {
		t.Errorf("re-imported streams differ:\n got %+v\nwant %+v", got.Streams, before.Streams)
	}
}
