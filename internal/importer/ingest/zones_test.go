package ingest

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/importer"
	"github.com/iskyd/kudos/internal/store/db"
)

// The corpus by date: pool swim (Nov 2025), strength (Mar 2026), bike and hike
// (Aug 2026), Zwift ride (no thresholds), intervals and half marathon (Oct 2026).
var corpusFiles = []string{"pool_swim.fit", "strength.fit", "bike.fit", "hike.fit", "zwift.fit", "intervals.fit", "half_marathon.fit"}

// suggestions lists a user's suggestions, pending and dismissed, as
// "sport day lthr/ftp/max/rest status", oldest first.
func (e env) suggestions(t *testing.T, userID int64) []string {
	t.Helper()
	rows, err := e.pool.Query(t.Context(), `SELECT sport, effective_from, lthr_bpm, ftp_w, max_hr_bpm, resting_hr_bpm, status
		FROM zone_suggestions WHERE user_id = $1 ORDER BY effective_from, sport`, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sport, status string
		var day time.Time
		var lthr, ftp, maxHR, rest *int16
		if err := rows.Scan(&sport, &day, &lthr, &ftp, &maxHR, &rest, &status); err != nil {
			t.Fatal(err)
		}
		v := func(p *int16) string {
			if p == nil {
				return "-"
			}
			return fmt.Sprint(*p)
		}
		out = append(out, fmt.Sprintf("%s %s %s/%s/%s/%s %s", sport, day.Format(time.DateOnly), v(lthr), v(ftp), v(maxHR), v(rest), status))
	}
	return out
}

// The thresholds the corpus files carry, one suggestion per change and sport.
// A run's FTP (378 W of running power) never appears, and no cycling suggestion
// comes from a run.
var corpusSuggestions = []string{
	"general 2025-11-19 -/-/194/58 pending",
	"general 2026-03-23 -/-/192/58 pending",
	"cycling 2026-08-07 185/-/205/58 pending",
	"general 2026-08-09 -/-/205/58 pending",
	"running 2026-10-15 171/-/187/58 pending",
}

func TestZoneSuggestionsFromTheCorpus(t *testing.T) {
	t.Parallel()
	newest := slices.Clone(corpusFiles)
	slices.Reverse(newest)
	for name, order := range map[string][]string{"oldest first": corpusFiles, "newest first": newest} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			for _, f := range order {
				e.upload(t, f)
			}
			if got := e.suggestions(t, e.alice); !slices.Equal(got, corpusSuggestions) {
				t.Errorf("suggestions:\n%v\nwant\n%v", got, corpusSuggestions)
			}
			// Importing again changes nothing; Bob sees none of it.
			for _, f := range order {
				e.upload(t, f)
			}
			if got := e.suggestions(t, e.alice); !slices.Equal(got, corpusSuggestions) {
				t.Errorf("after importing again: %v", got)
			}
			if got := e.suggestions(t, e.bob); len(got) != 0 {
				t.Errorf("bob's suggestions: %v", got)
			}
		})
	}
}

func TestZoneSuggestionsRespectVersionsAndDismissals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	// The running version already matches the runs; the general one predates the swim.
	_, err := e.q.CreateZoneSet(ctx, db.CreateZoneSetParams{
		UserID: e.alice, Sport: "running", EffectiveFrom: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		LthrBpm: new(int16(171)), MaxHrBpm: new(int16(187)), RestingHrBpm: new(int16(58)), ThresholdSpeedMS: new(float32(4)),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.q.CreateZoneSet(ctx, db.CreateZoneSetParams{
		UserID: e.alice, Sport: "general", EffectiveFrom: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxHrBpm: new(int16(194)), RestingHrBpm: new(int16(58)),
	})
	if err != nil {
		t.Fatal(err)
	}
	e.upload(t, "intervals.fit")
	e.upload(t, "pool_swim.fit")
	e.upload(t, "strength.fit")
	want := []string{"general 2026-03-23 -/-/192/58 pending"}
	if got := e.suggestions(t, e.alice); !slices.Equal(got, want) {
		t.Fatalf("suggestions: %v, want %v", got, want)
	}

	// A dismissed suggestion is not asked again.
	rows, err := e.q.ListZoneSuggestions(ctx, db.ListZoneSuggestionsParams{UserID: e.alice, MaxRows: 10})
	if err != nil || len(rows) != 1 {
		t.Fatalf("pending: %v, %v", rows, err)
	}
	if n, err := e.q.DismissZoneSuggestion(ctx, db.DismissZoneSuggestionParams{UserID: e.alice, ID: rows[0].ID}); err != nil || n != 1 {
		t.Fatalf("dismiss: %d, %v", n, err)
	}
	e.upload(t, "strength.fit")
	want = []string{"general 2026-03-23 -/-/192/58 dismissed"}
	if got := e.suggestions(t, e.alice); !slices.Equal(got, want) {
		t.Errorf("after dismissing: %v", got)
	}
}

func TestSuggestZonesEdgeCases(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	run := importer.Activity{Sport: importer.SportRunning, StartTime: time.Date(2026, 10, 15, 6, 0, 0, 0, time.UTC)}

	// A file without any threshold suggests nothing.
	run.Thresholds = &importer.Thresholds{}
	if err := suggestZones(ctx, e.q, e.alice, 0, run, 0); err != nil {
		t.Fatal(err)
	}
	if got := e.suggestions(t, e.alice); len(got) != 0 {
		t.Errorf("suggestions without thresholds: %v", got)
	}

	// Failures surface: an activity that does not exist, and a closed pool.
	run.Thresholds = &importer.Thresholds{ThresholdHR: new(171.0)}
	if err := suggestZones(ctx, e.q, e.alice, 999_999, run, 0); err == nil {
		t.Error("suggested for a missing activity")
	}
	broken := pg.NewEmptyDB(t)
	broken.Close()
	if err := suggestZones(ctx, db.New(broken), e.alice, 1, run, 0); err == nil {
		t.Error("suggested over a closed pool")
	}
}
