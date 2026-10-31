package ingest

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/importer"
	"github.com/iskyd/kudos/internal/store/db"
)

// The corpus by date: pool swim (Nov 2025), strength (Mar 2026), bike and hike
// (Aug 2026), Zwift ride (no thresholds), intervals and half marathon (Oct 2026).
var corpusFiles = []string{"pool_swim.fit", "strength.fit", "bike.fit", "hike.fit", "zwift.fit", "intervals.fit", "half_marathon.fit"}

// suggestions lists a user's suggestions, pending and dismissed, as
// "sport day lthr/ftp/max/rest status", oldest first, with the threshold pace
// in seconds per kilometre when one is suggested.
func (e env) suggestions(t *testing.T, userID int64) []string {
	t.Helper()
	rows, err := e.pool.Query(t.Context(), `SELECT sport, effective_from, lthr_bpm, ftp_w, max_hr_bpm, resting_hr_bpm, status,
		round(1000 / threshold_speed_m_s)::int FROM zone_suggestions WHERE user_id = $1 ORDER BY effective_from, sport`, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sport, status string
		var day time.Time
		var lthr, ftp, maxHR, rest *int16
		var pace *int32
		if err := rows.Scan(&sport, &day, &lthr, &ftp, &maxHR, &rest, &status, &pace); err != nil {
			t.Fatal(err)
		}
		v := func(p *int16) string {
			if p == nil {
				return "-"
			}
			return fmt.Sprint(*p)
		}
		line := fmt.Sprintf("%s %s %s/%s/%s/%s %s", sport, day.Format(time.DateOnly), v(lthr), v(ftp), v(maxHR), v(rest), status)
		if pace != nil {
			line += fmt.Sprintf(" %d s/km", *pace)
		}
		out = append(out, line)
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

func TestEstimatedThresholdsFromTheCorpus(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	ids := map[string]int64{}
	for _, f := range corpusFiles {
		ids[f] = e.upload(t, f).ID
	}
	// The Zwift ride's best 30 minutes (135.2 W) beat its best 20 (133.5 W):
	// FTP 128 W. The intervals' best 30 minutes at 3.66 m/s give 4:34/km and
	// join the file's running suggestion that day; the half marathon is slower.
	// The outdoor ride has no power. Recomputing changes nothing.
	want := slices.Clone(corpusSuggestions)
	want[4] += " 274 s/km"
	want = append(want, "cycling 2026-10-20 -/128/-/- pending")
	for range 2 {
		for _, f := range corpusFiles {
			e.compute(t, ids[f])
		}
		if got := e.suggestions(t, e.alice); !slices.Equal(got, want) {
			t.Fatalf("suggestions:\n%v\nwant\n%v", got, want)
		}
	}
	if got := e.suggestions(t, e.bob); len(got) != 0 {
		t.Errorf("bob's suggestions: %v", got)
	}

	// A dismissed estimate is not suggested again, nor a lower one.
	rows, err := e.q.ListZoneSuggestions(ctx, db.ListZoneSuggestionsParams{UserID: e.alice, MaxRows: 1})
	if err != nil || len(rows) != 1 || rows[0].FtpW == nil {
		t.Fatalf("latest: %v, %v", rows, err)
	}
	if _, err := e.q.DismissZoneSuggestion(ctx, db.DismissZoneSuggestionParams{UserID: e.alice, ID: rows[0].ID}); err != nil {
		t.Fatal(err)
	}
	e.compute(t, ids["zwift.fit"])
	if got := e.suggestions(t, e.alice); got[len(got)-1] != "cycling 2026-10-20 -/128/-/- dismissed" || len(got) != len(want) {
		t.Errorf("after dismissing: %v", got)
	}
}

func TestEstimatesAgainstVersionsAndFiles(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	zwift := e.upload(t, "zwift.fit")
	_, err := e.q.CreateZoneSet(ctx, db.CreateZoneSetParams{
		UserID: e.alice, Sport: "cycling", EffectiveFrom: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		LthrBpm: new(int16(185)), FtpW: new(int16(120)),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 128 W beats the version's 120 W.
	e.compute(t, zwift.ID)
	want := []string{"cycling 2026-10-20 -/128/-/- pending"}
	if got := e.suggestions(t, e.alice); !slices.Equal(got, want) {
		t.Fatalf("suggestions: %v, want %v", got, want)
	}
	// A later file with the version's LTHR suggests nothing: the estimate only
	// adds an FTP over the version's values.
	ride := importer.Activity{
		Sport: importer.SportCycling, StartTime: time.Date(2026, 10, 22, 6, 0, 0, 0, time.UTC),
		Thresholds: &importer.Thresholds{ThresholdHR: new(185.0)},
	}
	if err := suggestZones(ctx, e.q, e.alice, zwift.ID, ride, 0); err != nil {
		t.Fatal(err)
	}
	if got := e.suggestions(t, e.alice); !slices.Equal(got, want) {
		t.Errorf("after a file with known values: %v", got)
	}

	// Confirming applies it; a version above the estimate leaves nothing to suggest.
	rows, err := e.q.ListZoneSuggestions(ctx, db.ListZoneSuggestionsParams{UserID: e.alice, MaxRows: 1})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	_, err = e.q.ConfirmZoneSuggestion(ctx, db.ConfirmZoneSuggestionParams{
		UserID: e.alice, ID: rows[0].ID, Sport: "cycling", EffectiveFrom: rows[0].EffectiveFrom, LthrBpm: new(int16(185)), FtpW: rows[0].FtpW,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.compute(t, zwift.ID)
	if got := e.suggestions(t, e.alice); len(got) != 0 {
		t.Errorf("after confirming: %v", got)
	}

	// Failures surface.
	broken := pg.NewEmptyDB(t)
	broken.Close()
	curve := []analytics.CurvePoint{{Kind: analytics.PowerCurve, DurationS: analytics.FTPDurationS, Value: 300}}
	if err := suggestEstimates(ctx, db.New(broken), e.alice, zwift.ID, time.Now(), curve); err == nil {
		t.Error("suggested over a closed pool")
	}
	if err := suggestEstimates(ctx, e.q, e.alice, 999_999, time.Now(), curve); err == nil {
		t.Error("suggested for a missing activity")
	}
}
