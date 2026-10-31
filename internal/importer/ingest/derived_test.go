package ingest

import (
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/importer"
	"github.com/iskyd/kudos/internal/jobs"
	"github.com/iskyd/kudos/internal/store/db"
)

func (e env) derived(t *testing.T, id int64) db.ActivityDerived {
	t.Helper()
	d, err := e.q.GetActivityDerived(t.Context(), db.GetActivityDerivedParams{UserID: e.alice, ActivityID: id})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (e env) compute(t *testing.T, id int64) db.ActivityDerived {
	t.Helper()
	if err := e.svc.ComputeDerived(t.Context(), e.alice, id); err != nil {
		t.Fatal(err)
	}
	return e.derived(t, id)
}

// jobCount counts the queued jobs of a kind.
func (e env) jobCount(t *testing.T, kind string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM river_job WHERE kind = $1", kind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func sum(vs []float64) float64 {
	total := 0.0
	for _, v := range vs {
		total += v
	}
	return total
}

func TestDerivedValuesAreTimeWeighted(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a := e.upload(t, "strength.fit")

	// The strength sample records HR at irregular intervals: weight each sample
	// by the time to the next (none across a pause over 10 s), by hand.
	rows := e.viewSamples(t, a.ID)
	gaps := map[time.Duration]bool{}
	var weighted, weight, plain float64
	n := 0
	for i, r := range rows {
		if r.HrBpm == nil {
			continue
		}
		plain += float64(*r.HrBpm)
		n++
		if i+1 < len(rows) {
			gap := rows[i+1].Time.Sub(r.Time)
			gaps[gap] = true
			if gap <= 10*time.Second {
				weighted += float64(*r.HrBpm) * gap.Seconds()
				weight += gap.Seconds()
			}
		}
	}
	if len(gaps) < 3 {
		t.Fatalf("sampling looks regular: gaps %v", gaps)
	}
	d := e.compute(t, a.ID)
	if d.AvgHrBpm == nil || math.Abs(*d.AvgHrBpm-weighted/weight) > 1e-9 {
		t.Errorf("avg HR %v, time-weighted %v", d.AvgHrBpm, weighted/weight)
	}
	if math.Abs(*d.AvgHrBpm-plain/float64(n)) < 0.1 {
		t.Errorf("time-weighted %v is the plain mean %v: the test proves nothing", *d.AvgHrBpm, plain/float64(n))
	}
	if d.Version != derivedVersion || d.Stale || d.HrZoneModel != nil || d.HrZoneS != nil {
		t.Errorf("without zones: %+v", d)
	}

	// With general max and resting HR, a strength session gets heart rate reserve
	// zones (general is its own sport) whose seconds add up to the weighted time.
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	err := e.svc.ChangeZones(t.Context(), e.alice, func(q *db.Queries) (time.Time, error) {
		_, err := q.CreateZoneSet(t.Context(), db.CreateZoneSetParams{
			UserID: e.alice, Sport: "general", EffectiveFrom: from, MaxHrBpm: new(int16(192)), RestingHrBpm: new(int16(58)),
		})
		return from, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RecomputeDerived(t.Context(), e.alice); err != nil {
		t.Fatal(err)
	}
	d = e.derived(t, a.ID)
	if *d.HrZoneModel != "heart_rate_reserve" || len(d.HrZoneS) != 5 || math.Abs(sum(d.HrZoneS)-weight) > 1e-6 || d.ZoneSetID == nil {
		t.Errorf("time in zones %v (model %v), weighted time %v", d.HrZoneS, d.HrZoneModel, weight)
	}
}

func TestZoneChangesInvalidateFromTheirDate(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	run := e.upload(t, "intervals.fit") // 15 October 2026
	e.compute(t, run.ID)

	version := func(from time.Time) db.ZoneSet {
		var z db.ZoneSet
		err := e.svc.ChangeZones(ctx, e.alice, func(q *db.Queries) (time.Time, error) {
			var err error
			z, err = q.CreateZoneSet(ctx, db.CreateZoneSetParams{
				UserID: e.alice, Sport: "running", EffectiveFrom: from, LthrBpm: new(int16(171)), ThresholdSpeedMS: new(float32(4.2)),
			})
			return from, err
		})
		if err != nil {
			t.Fatal(err)
		}
		return z
	}
	// A version after the run changes nothing for it.
	version(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if e.derived(t, run.ID).Stale {
		t.Error("a later version made the run stale")
	}
	if n, err := e.svc.RecomputeDerived(ctx, e.alice); err != nil || n != 0 {
		t.Errorf("recomputed %d: %v", n, err)
	}

	// A version from before it does, and queues a recompute.
	before := version(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if !e.derived(t, run.ID).Stale || e.jobCount(t, jobs.RecomputeArgs{}.Kind()) != 2 {
		t.Fatal("the run is not stale, or no recompute was queued")
	}
	if n, err := e.svc.RecomputeDerived(ctx, e.alice); err != nil || n != 1 {
		t.Fatalf("recomputed %d: %v", n, err)
	}
	d := e.derived(t, run.ID)
	if d.Stale || *d.ZoneSetID != before.ID || *d.HrZoneModel != "friel_run_hr" || *d.PaceZoneModel != "friel_run_pace" ||
		len(d.HrZoneS) != 7 || sum(d.HrZoneS) < 2000 || sum(d.PaceZoneS) < 2000 || d.PowerZoneS != nil {
		t.Errorf("with running zones: %+v", d)
	}

	// Deleting it makes the run stale again; recomputed, it has no zones.
	err := e.svc.ChangeZones(ctx, e.alice, func(q *db.Queries) (time.Time, error) {
		return q.DeleteZoneSet(ctx, db.DeleteZoneSetParams{UserID: e.alice, ID: before.ID})
	})
	if err != nil || !e.derived(t, run.ID).Stale {
		t.Fatalf("after deleting: %v", err)
	}
	if _, err := e.svc.RecomputeDerived(ctx, e.alice); err != nil {
		t.Fatal(err)
	}
	if d := e.derived(t, run.ID); d.Stale || d.HrZoneModel != nil || d.ZoneSetID != nil {
		t.Errorf("after deleting the version: %+v", d)
	}

	// A failed change is returned as is.
	boom := errors.New("boom")
	if err := e.svc.ChangeZones(ctx, e.alice, func(*db.Queries) (time.Time, error) { return time.Time{}, boom }); !errors.Is(err, boom) {
		t.Errorf("ChangeZones = %v", err)
	}
}

func TestEditsRecomputeAndRecomputeIsIdempotent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	run := e.upload(t, "intervals.fit")
	_, err := e.q.CreateZoneSet(ctx, db.CreateZoneSetParams{UserID: e.alice, Sport: "running", EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), LthrBpm: new(int16(171))})
	if err != nil {
		t.Fatal(err)
	}
	first := e.compute(t, run.ID)
	again := e.compute(t, run.ID)
	if !sameDerived(first, again) {
		t.Errorf("recomputing changed the values:\n%+v\n%+v", first, again)
	}

	// An edit queues the activity's derived values; computing them follows the edit.
	queued := e.jobCount(t, jobs.DerivedArgs{}.Kind())
	e.add(t, run.ID, Edit{Kind: EditTrim, From: ptrTime(run.StartTime.Add(10 * time.Minute)), To: ptrTime(run.StartTime.Add(20 * time.Minute))})
	if e.jobCount(t, jobs.DerivedArgs{}.Kind()) != queued+1 {
		t.Error("the edit queued no derived values")
	}
	trimmed := e.compute(t, run.ID)
	if total := sum(trimmed.HrZoneS); total > 600 || total < 500 || *trimmed.AvgHrBpm == *first.AvgHrBpm {
		t.Errorf("after trimming to 10 minutes: %v s in zones, avg HR %v (was %v)", total, *trimmed.AvgHrBpm, *first.AvgHrBpm)
	}

	// A newer computation version recomputes everything once.
	if _, err := e.pool.Exec(ctx, "UPDATE activity_derived SET version = 0 WHERE user_id = $1", e.alice); err != nil {
		t.Fatal(err)
	}
	for pass, want := range []int{1, 0} {
		if n, err := e.svc.RecomputeDerived(ctx, e.alice); err != nil || n != want {
			t.Errorf("pass %d recomputed %d, want %d: %v", pass, n, want, err)
		}
	}
	// A missing activity is nothing to compute.
	if err := e.svc.ComputeDerived(ctx, e.alice, 999_999); err != nil {
		t.Errorf("missing activity: %v", err)
	}
}

// sameDerived compares two computations of an activity's values.
func sameDerived(a, b db.ActivityDerived) bool {
	eq := func(x, y *float64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a.Version == b.Version && a.Stale == b.Stale && eq(a.AvgHrBpm, b.AvgHrBpm) && eq(a.AvgSpeedMS, b.AvgSpeedMS) &&
		eq(a.AvgPowerW, b.AvgPowerW) && eq(a.AvgCadence, b.AvgCadence) && slices.Equal(a.HrZoneS, b.HrZoneS) &&
		slices.Equal(a.PaceZoneS, b.PaceZoneS) && slices.Equal(a.PowerZoneS, b.PowerZoneS) &&
		eq(a.Load, b.Load) && (a.LoadMethod == nil) == (b.LoadMethod == nil) && (a.LoadMethod == nil || *a.LoadMethod == *b.LoadMethod)
}

func TestLoadUsesTheFirstApplicableMethod(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	for _, p := range []db.CreateZoneSetParams{
		{Sport: "cycling", FtpW: new(int16(250))},
		{Sport: "running", ThresholdSpeedMS: new(float32(4))},
		{Sport: "swimming", CssMS: new(float32(1))},
		{Sport: "general", LthrBpm: new(int16(160)), RestingHrBpm: new(int16(50))},
	} {
		p.UserID, p.EffectiveFrom = e.alice, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		if _, err := e.q.CreateZoneSet(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	hours := func(a db.Activity) float64 { return *a.TimerTimeS / 3600 }
	speed := func(a db.Activity) float64 { return *a.DistanceM / *a.TimerTimeS }

	ride := e.upload(t, "zwift.fit")
	segs, err := e.svc.Segments(ctx, e.alice, View{Segments: []Segment{{ActivityID: ride.ID, From: minTime, To: maxTime}}}, maxSummarySamples)
	if err != nil {
		t.Fatal(err)
	}
	np, _ := analytics.NormalizedPower(segs)
	run := e.upload(t, "intervals.fit")
	swim := e.upload(t, "pool_swim.fit")
	strength := e.upload(t, "strength.fit")
	manual, err := e.svc.CreateManual(ctx, e.alice, Manual{
		Sport: importer.SportStrength, Start: time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC), Duration: 5400, RPE: new(6.0),
	})
	if err != nil {
		t.Fatal(err)
	}
	strengthHR := *e.compute(t, strength.ID).AvgHrBpm

	for _, tc := range []struct {
		name   string
		id     int64
		method string
		want   float64
	}{
		{"zwift: power against the cycling FTP", ride.ID, "power_tss", hours(e.effective(t, ride.ID)) * np * np / (250 * 250) * 100},
		// The run carries running power but no running FTP is set.
		{"intervals: pace", run.ID, "pace_rtss", hours(e.effective(t, run.ID)) * math.Pow(speed(e.effective(t, run.ID))/4, 2) * 100},
		// Speed from the lengths' distance over timer time.
		{"pool: CSS", swim.ID, "swim_tss", hours(e.effective(t, swim.ID)) * math.Pow(speed(e.effective(t, swim.ID)), 3) * 100},
		{"strength: heart rate", strength.ID, "hr_tss", hours(e.effective(t, strength.ID)) * (strengthHR - 50) * (strengthHR - 50) / (110 * 110) * 100},
		// 6 × 1.5 h × 10.
		{"manual: session RPE", manual, "session_rpe", 90},
	} {
		d := e.compute(t, tc.id)
		if d.LoadMethod == nil || *d.LoadMethod != tc.method || d.Load == nil || math.Abs(*d.Load-tc.want) > 1e-6 {
			t.Errorf("%s: %v %v, want %s %v", tc.name, deref(d.LoadMethod), deref(d.Load), tc.method, tc.want)
		}
	}
	if np <= 0 || speed(e.effective(t, swim.ID)) <= 0 {
		t.Errorf("degenerate inputs: NP %v", np)
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestDerivedWorkers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	run := e.upload(t, "intervals.fit")
	w := &Workers{Service: e.svc}
	driver := riverpgxv5.New(e.pool)
	done := func(res *rivertest.WorkResult, err error) {
		t.Helper()
		if err != nil || res.EventKind != river.EventKindJobCompleted {
			t.Fatalf("work: %v", err)
		}
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after the test is best effort

	done(rivertest.NewWorker(t, driver, &river.Config{}, &derivedWorker{w: w}).Work(ctx, t, tx, jobs.DerivedArgs{UserID: e.alice, ActivityID: run.ID}, nil))
	if _, err := e.q.GetActivityDerived(ctx, db.GetActivityDerivedParams{UserID: e.alice, ActivityID: run.ID}); err != nil {
		t.Errorf("the derived worker stored nothing: %v", err)
	}
	done(rivertest.NewWorker(t, driver, &river.Config{}, &recomputeWorker{w: w}).Work(ctx, t, tx, jobs.RecomputeArgs{UserID: e.alice}, nil))
	done(rivertest.NewWorker(t, driver, &river.Config{}, &recomputeAllWorker{w: w}).Work(ctx, t, tx, jobs.RecomputeAllArgs{}, nil))
	// One recompute per user: Alice and Bob.
	if n := e.jobCount(t, jobs.RecomputeArgs{}.Kind()); n != 2 {
		t.Errorf("recompute-all queued %d", n)
	}
}

func TestDerivedOnBrokenData(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	run := e.upload(t, "intervals.fit")

	// Corrupt overrides fail the computation instead of storing wrong values.
	if _, err := e.pool.Exec(ctx, `UPDATE activities SET overrides = '{"start_time": 1}' WHERE id = $1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ComputeDerived(ctx, e.alice, run.ID); err == nil {
		t.Error("computed over corrupt overrides")
	}
	if _, err := e.svc.RecomputeDerived(ctx, e.alice); err == nil {
		t.Error("recomputed over corrupt overrides")
	}

	pool := pg.NewEmptyDB(t)
	pool.Close()
	broken := New(pool, e.svc.jobs, e.dir)
	if err := broken.ComputeDerived(ctx, e.alice, run.ID); err == nil {
		t.Error("computed over a closed pool")
	}
	if _, err := broken.RecomputeDerived(ctx, e.alice); err == nil {
		t.Error("recomputed over a closed pool")
	}
	if err := broken.recomputeAll(ctx); err == nil {
		t.Error("queued recomputes over a closed pool")
	}
	if _, err := broken.ResolveZones(ctx, e.get(t, run.ID)); err == nil {
		t.Error("resolved zones over a closed pool")
	}
	if err := broken.ChangeZones(ctx, e.alice, func(*db.Queries) (time.Time, error) { return time.Time{}, nil }); err == nil {
		t.Error("changed zones over a closed pool")
	}
}

func TestBestEffortsAreStoredWithDerivedValues(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	run := e.upload(t, "half_marathon.fit")
	zwift := e.upload(t, "zwift.fit")
	ride := e.upload(t, "bike.fit")
	records := func(userID int64) map[string][]int64 {
		t.Helper()
		rows, err := e.q.ListRecordHistory(ctx, db.ListRecordHistoryParams{UserID: userID, MaxRows: 100})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string][]int64{}
		for _, r := range rows {
			out[r.Effort] = append(out[r.Effort], r.ActivityID)
			if r.Effort == string(analytics.LongestRun) && r.Value != *e.effective(t, run.ID).DistanceM {
				t.Errorf("longest run %v m", r.Value)
			}
		}
		return out
	}
	for range 2 { // recomputing replaces the rows
		for _, id := range []int64{run.ID, zwift.ID, ride.ID} {
			e.compute(t, id)
		}
	}
	got := records(e.alice)
	for _, effort := range []analytics.EffortName{
		analytics.Fastest1K, analytics.Fastest5K, analytics.Fastest10K, analytics.FastestHalfMarathon, analytics.LongestRun,
	} {
		if !slices.Equal(got[string(effort)], []int64{run.ID}) {
			t.Errorf("%s: %v", effort, got[string(effort)])
		}
	}
	// The virtual ride sets power records but never a distance one.
	if !slices.Equal(got[string(analytics.LongestRide)], []int64{ride.ID}) || !slices.Contains(got[string(analytics.Power5s)], zwift.ID) {
		t.Errorf("rides: %v", got)
	}
	if bobs := records(e.bob); len(bobs) != 0 {
		t.Errorf("bob's records: %v", bobs)
	}

	// Efforts follow edits: a run changed to a ride sets no running records.
	e.add(t, run.ID, Edit{Kind: EditSport, Sport: "cycling", Subtype: "outdoor"})
	e.compute(t, run.ID)
	if got := records(e.alice); got[string(analytics.Fastest1K)] != nil || got[string(analytics.LongestRun)] != nil {
		t.Errorf("after the sport change: %v", got)
	}
}

func TestCurvePointsAreStoredWithDerivedValues(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	run, zwift := e.upload(t, "half_marathon.fit"), e.upload(t, "zwift.fit")
	curves := func(userID int64) map[string]int64 {
		t.Helper()
		rows, err := e.q.ListBestCurvePoints(t.Context(), db.ListBestCurvePointsParams{UserID: userID, MaxRows: 100})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]int64{}
		for _, r := range rows {
			out[r.Kind] = r.ActivityID
		}
		return out
	}
	for range 2 { // recomputing replaces the rows
		e.compute(t, run.ID)
		e.compute(t, zwift.ID)
	}
	if got := curves(e.alice); len(got) != 2 || got["pace"] != run.ID || got["power"] != zwift.ID {
		t.Errorf("curves: %v", got)
	}
	if got := curves(e.bob); len(got) != 0 {
		t.Errorf("bob's curves: %v", got)
	}
	// Curves follow edits: a run changed to a ride loses its pace curve.
	e.add(t, run.ID, Edit{Kind: EditSport, Sport: "cycling", Subtype: "outdoor"})
	e.compute(t, run.ID)
	if got := curves(e.alice); len(got) != 1 || got["pace"] != 0 {
		t.Errorf("after the sport change: %v", got)
	}
}

func TestRunningAndSwimAnalyticsAreStored(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	run, swim, ride := e.upload(t, "half_marathon.fit"), e.upload(t, "pool_swim.fit"), e.upload(t, "zwift.fit")
	nearly := func(name string, got *float64, want float64) {
		t.Helper()
		if got == nil || math.Abs(*got-want) > 1e-3 {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	// The half marathon: 1.228 m per beat, heart rate drifting 9.3% against
	// pace, and its running dynamics (time-weighted, in metres and seconds).
	d := e.compute(t, run.ID)
	nearly("EF", d.EfficiencyMPerBeat, 1.2282)
	nearly("decoupling", d.DecouplingPct, 9.3104)
	nearly("vertical oscillation", d.AvgVerticalOscillationM, 0.1132)
	nearly("stance time", d.AvgStanceTimeS, 0.2209)
	nearly("step length", d.AvgStepLengthM, 1.2071)
	if d.AvgSwolf != nil {
		t.Errorf("run SWOLF: %v", *d.AvgSwolf)
	}
	// The pool swim: SWOLF from its active lengths and a 200 m from 50 m
	// lengths in a row, but no 400 m without a rest.
	d = e.compute(t, swim.ID)
	nearly("SWOLF", d.AvgSwolf, 109.0562)
	if d.EfficiencyMPerBeat != nil || d.DecouplingPct != nil {
		t.Errorf("swim running analytics: %+v", d)
	}
	swimEfforts := func() map[string]float64 {
		t.Helper()
		rows, err := e.q.ListBestEffortsInPeriod(t.Context(), db.ListBestEffortsInPeriodParams{
			UserID: e.alice, Efforts: []string{string(analytics.FastestSwim200), string(analytics.FastestSwim400)},
		})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]float64{}
		for _, r := range rows {
			out[r.Effort] = r.Value
		}
		return out
	}
	if got := swimEfforts(); len(got) != 1 || got[string(analytics.FastestSwim200)] != 313.5 {
		t.Errorf("swim efforts: %v", got)
	}
	if d := e.compute(t, ride.ID); d.EfficiencyMPerBeat != nil || d.AvgSwolf != nil || d.AvgStanceTimeS != nil {
		t.Errorf("ride: %+v", d)
	}

	// They follow edits: trimmed to its first 4 minutes the swim keeps a SWOLF
	// but no 200 m; a run changed to a virtual run has no running analytics.
	from, to := swim.StartTime, swim.StartTime.Add(4*time.Minute)
	e.add(t, swim.ID, Edit{Kind: EditTrim, From: &from, To: &to})
	if d := e.compute(t, swim.ID); d.AvgSwolf == nil || *d.AvgSwolf == 109.0562 {
		t.Errorf("trimmed SWOLF: %v", d.AvgSwolf)
	}
	if got := swimEfforts(); len(got) != 0 {
		t.Errorf("trimmed swim efforts: %v", got)
	}
	e.add(t, run.ID, Edit{Kind: EditSport, Sport: "running", Subtype: "virtual"})
	if d := e.compute(t, run.ID); d.EfficiencyMPerBeat != nil || d.DecouplingPct != nil || d.AvgStepLengthM != nil {
		t.Errorf("virtual run: %+v", d)
	}
}
