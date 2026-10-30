package ingest

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/store/db"
)

func (e env) upload(t *testing.T, name string) db.Activity {
	t.Helper()
	ids, err := e.svc.Upload(t.Context(), e.alice, time.UTC, name, corpus(t, "fit/"+name))
	if err != nil {
		t.Fatal(err)
	}
	return e.get(t, ids[0])
}

func (e env) get(t *testing.T, id int64) db.Activity {
	t.Helper()
	a, err := e.q.GetActivity(t.Context(), db.GetActivityParams{UserID: e.alice, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (e env) effective(t *testing.T, id int64) db.Activity {
	t.Helper()
	a, err := Effective(e.get(t, id))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// streamHash fingerprints every stored sample of an activity.
func (e env) streamHash(t *testing.T, id int64) [32]byte {
	t.Helper()
	rows, err := e.q.ListActivityStreams(t.Context(), db.ListActivityStreamsParams{UserID: e.alice, ActivityID: id, MaxRows: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

func (e env) viewSamples(t *testing.T, id int64) []db.ActivityStream {
	t.Helper()
	v, err := e.svc.View(t.Context(), e.get(t, id))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := e.svc.Samples(t.Context(), e.alice, v, nil, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func (e env) edits(t *testing.T, id int64) []db.ActivityEdit {
	t.Helper()
	rows, err := e.q.ListActivityEdits(t.Context(), db.ListActivityEditsParams{UserID: e.alice, ActivityID: id})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func (e env) add(t *testing.T, id int64, ed Edit) {
	t.Helper()
	if err := e.svc.AddEdit(t.Context(), e.alice, id, ed); err != nil {
		t.Fatal(err)
	}
}

func (e env) undoAll(t *testing.T, id int64) {
	t.Helper()
	for _, ed := range e.edits(t, id) {
		if err := e.svc.UndoEdit(t.Context(), e.alice, id, ed.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func wantInvalid(t *testing.T, err error, want string) {
	t.Helper()
	var invalid InvalidEditError
	if !errors.As(err, &invalid) || string(invalid) != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestEditsAreReversible(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	a := e.upload(t, "intervals.fit")
	hash := e.streamHash(t, a.ID)
	recorded := len(e.viewSamples(t, a.ID))

	e.add(t, a.ID, Edit{Kind: EditSport, Sport: "cycling", Subtype: "indoor"})
	if got := e.effective(t, a.ID); got.Sport != "cycling" || got.Subtype != "indoor" || *got.DistanceM != *a.DistanceM {
		t.Errorf("after the sport change: %s/%s, %v m", got.Sport, got.Subtype, *got.DistanceM)
	}
	cycling := "cycling"
	if rows, err := e.q.ListActivities(ctx, db.ListActivitiesParams{
		UserID: e.alice, Sort: "date", Sport: &cycling, BeforeKey: 1e300, BeforeID: 1 << 62, MaxRows: 10,
	}); err != nil || len(rows) != 1 {
		t.Errorf("sport filter after the change: %d, %v", len(rows), err)
	}

	from, to := a.StartTime.Add(10*time.Minute), a.StartTime.Add(20*time.Minute)
	e.add(t, a.ID, Edit{Kind: EditTrim, From: &from, To: &to})
	got := e.effective(t, a.ID)
	if got.StartTime.Before(from) || *got.ElapsedTimeS > 600 || *got.ElapsedTimeS < 590 ||
		*got.DistanceM >= *a.DistanceM || got.CaloriesKcal != nil || got.AvgHrBpm == nil {
		t.Errorf("after trimming: start %v, elapsed %v, distance %v, kcal %v", got.StartTime, *got.ElapsedTimeS, *got.DistanceM, got.CaloriesKcal)
	}
	for _, r := range e.viewSamples(t, a.ID) {
		if r.Time.Before(from) || r.Time.After(to) {
			t.Fatalf("sample at %v outside the trim", r.Time)
		}
	}

	e.add(t, a.ID, Edit{Kind: EditExcludeHR, From: &from, To: &to})
	if got := e.effective(t, a.ID); got.AvgHrBpm != nil || got.MaxHrBpm != nil {
		t.Errorf("heart rate after excluding all of it: %v, %v", got.AvgHrBpm, got.MaxHrBpm)
	}
	for _, r := range e.viewSamples(t, a.ID) {
		if r.HrBpm != nil {
			t.Fatalf("heart rate kept at %v", r.Time)
		}
	}
	if e.streamHash(t, a.ID) != hash {
		t.Fatal("editing changed the stored samples")
	}

	// Undo the trim alone: heart rate stays excluded in that range, the rest is back.
	trim := e.edits(t, a.ID)[1]
	if err := e.svc.UndoEdit(ctx, e.alice, a.ID, trim.ID); err != nil {
		t.Fatal(err)
	}
	got = e.effective(t, a.ID)
	if !got.StartTime.Equal(a.StartTime) || *got.DistanceM != *a.DistanceM || *got.AvgHrBpm == *a.AvgHrBpm || got.Sport != "cycling" {
		t.Errorf("after undoing the trim: %+v", got)
	}
	if n := len(e.viewSamples(t, a.ID)); n != recorded {
		t.Errorf("%d samples after undoing the trim, want %d", n, recorded)
	}

	e.undoAll(t, a.ID)
	if got := e.get(t, a.ID); string(got.Overrides) != "{}" {
		t.Errorf("overrides after undoing everything: %s", got.Overrides)
	}
	if got := e.effective(t, a.ID); got.Sport != a.Sport || *got.AvgHrBpm != *a.AvgHrBpm {
		t.Errorf("after undoing everything: %+v", got)
	}
	if e.streamHash(t, a.ID) != hash {
		t.Fatal("undoing changed the stored samples")
	}
}

func TestEditErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	a := e.upload(t, "intervals.fit")
	start := a.StartTime

	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditTrim, From: &start, To: &start}), "the range must end after it starts")
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditExcludeHR}), "the range must end after it starts")
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: "rotate"}), "unknown edit")
	late, later := start.Add(24*time.Hour), start.Add(25*time.Hour)
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditTrim, From: &late, To: &later}), "nothing would be left of the recorded samples")

	if err := e.svc.AddEdit(ctx, e.bob, a.ID, Edit{Kind: EditSport, Sport: "cycling", Subtype: "outdoor"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob edits: %v", err)
	}
	e.add(t, a.ID, Edit{Kind: EditSport, Sport: "cycling", Subtype: "outdoor"})
	ed := e.edits(t, a.ID)[0]
	if err := e.svc.UndoEdit(ctx, e.bob, a.ID, ed.ID); !errors.Is(err, ErrEditNotFound) {
		t.Errorf("bob undoes: %v", err)
	}
	if err := e.svc.UndoEdit(ctx, e.alice, a.ID, ed.ID+1000); !errors.Is(err, ErrEditNotFound) {
		t.Errorf("unknown edit: %v", err)
	}
	for range MaxEdits - 1 {
		e.add(t, a.ID, Edit{Kind: EditSport, Sport: "running", Subtype: "outdoor"})
	}
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditSport, Sport: "running", Subtype: "outdoor"}),
		fmt.Sprintf("an activity holds at most %d edits", MaxEdits))
	// The latest sport change wins.
	if got := e.effective(t, a.ID); got.Sport != "running" {
		t.Errorf("sport = %s", got.Sport)
	}
}

func TestMergeThenUndo(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	a := e.upload(t, "intervals.fit")
	b := e.upload(t, "half_marathon.fit")
	hashA, hashB := e.streamHash(t, a.ID), e.streamHash(t, b.ID)
	countA, countB := len(e.viewSamples(t, a.ID)), len(e.viewSamples(t, b.ID))

	e.add(t, a.ID, Edit{Kind: EditMerge, ActivityID: b.ID})
	if listed := e.activities(t, e.alice); len(listed) != 1 || listed[0].ID != a.ID {
		t.Errorf("listed after merging: %d", len(listed))
	}
	if got := e.get(t, b.ID); got.MergedInto == nil || *got.MergedInto != a.ID {
		t.Errorf("merged into %v", got.MergedInto)
	}
	merged := e.viewSamples(t, a.ID)
	if len(merged) != countA+countB {
		t.Errorf("%d samples merged, want %d", len(merged), countA+countB)
	}
	for i := 1; i < len(merged); i++ {
		if !merged[i].Time.After(merged[i-1].Time) {
			t.Fatalf("merged samples out of order at %d", i)
		}
	}
	v, err := e.svc.View(ctx, e.get(t, a.ID))
	if err != nil {
		t.Fatal(err)
	}
	segs, err := e.svc.Segments(ctx, e.alice, v, 1_000_000)
	if err != nil || len(segs) != 2 || len(segs[0])+len(segs[1]) != countA+countB || segs[0][0].Altitude == nil {
		t.Errorf("segments: %d, %v", len(segs), err)
	}
	got := e.effective(t, a.ID)
	first := a
	if b.StartTime.Before(a.StartTime) {
		first = b
	}
	if !got.StartTime.Equal(first.StartTime) || *got.DistanceM < *a.DistanceM+*b.DistanceM-100 || *got.DistanceM > *a.DistanceM+*b.DistanceM+100 {
		t.Errorf("merged: start %v, distance %v (recorded %v + %v)", got.StartTime, *got.DistanceM, *a.DistanceM, *b.DistanceM)
	}

	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, b.ID, Edit{Kind: EditSport, Sport: "cycling", Subtype: "outdoor"}),
		"this activity was merged into another; edit that one")
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditMerge, ActivityID: b.ID}),
		"an activity that was merged or split cannot be merged again")

	e.undoAll(t, a.ID)
	if listed := e.activities(t, e.alice); len(listed) != 2 {
		t.Errorf("listed after undoing: %d", len(listed))
	}
	for _, x := range []db.Activity{a, b} {
		if got := e.get(t, x.ID); got.MergedInto != nil || string(got.Overrides) != "{}" {
			t.Errorf("activity %d after undoing: merged into %v, overrides %s", x.ID, got.MergedInto, got.Overrides)
		}
	}
	if len(e.viewSamples(t, a.ID)) != countA || e.streamHash(t, a.ID) != hashA || e.streamHash(t, b.ID) != hashB {
		t.Error("samples changed")
	}

	// The other way round: samples still come in time order.
	e.add(t, b.ID, Edit{Kind: EditMerge, ActivityID: a.ID})
	merged = e.viewSamples(t, b.ID)
	for i := 1; i < len(merged); i++ {
		if !merged[i].Time.After(merged[i-1].Time) {
			t.Fatalf("merged samples out of order at %d", i)
		}
	}
	e.undoAll(t, b.ID)
}

func TestMergeErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	a := e.upload(t, "intervals.fit")
	manual := func(id string, start time.Time) int64 {
		n, err := e.q.UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: e.alice, Source: "manual", SourceActivityID: id, Sport: "running", Subtype: "outdoor", StartTime: start, ElapsedTimeS: new(600.0),
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditMerge, ActivityID: a.ID}), "an activity cannot be merged with itself")
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditMerge, ActivityID: a.ID + 1000}), "no such activity to merge")
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditMerge, ActivityID: manual("during", a.StartTime.Add(time.Minute))}),
		"the activities overlap in time")
	empty := manual("next day", a.StartTime.Add(24*time.Hour))
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditMerge, ActivityID: empty}), "both activities need recorded samples")
	if got := e.get(t, empty); got.MergedInto != nil {
		t.Error("a refused merge was kept")
	}
	// No duration: it starts and ends at once, after a.
	instant, err := e.q.UpsertActivity(ctx, db.UpsertActivityParams{
		UserID: e.alice, Source: "manual", SourceActivityID: "instant", Sport: "running", Subtype: "outdoor", StartTime: a.StartTime.Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditMerge, ActivityID: instant}), "both activities need recorded samples")
	// A split activity cannot join a merge.
	hike := e.upload(t, "hike.fit")
	e.add(t, hike.ID, Edit{Kind: EditSplit, At: ptrTime(hike.StartTime.Add(10 * time.Minute))})
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditMerge, ActivityID: hike.ID}),
		"an activity that was merged or split cannot be merged again")
	// Bob's activity is not there to merge.
	ids, err := e.svc.Upload(ctx, e.bob, time.UTC, "hike.fit", corpus(t, "fit/hike.fit"))
	if err != nil {
		t.Fatal(err)
	}
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditMerge, ActivityID: ids[0]}), "no such activity to merge")
}

func TestSplitThenUndo(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	a := e.upload(t, "intervals.fit")
	hash := e.streamHash(t, a.ID)
	total := len(e.viewSamples(t, a.ID))
	at := a.StartTime.Add(30 * time.Minute)

	e.add(t, a.ID, Edit{Kind: EditSport, Sport: "trail_running", Subtype: "outdoor"})
	e.add(t, a.ID, Edit{Kind: EditSplit, At: &at})
	listed := e.activities(t, e.alice)
	if len(listed) != 2 {
		t.Fatalf("listed after splitting: %d", len(listed))
	}
	split := e.edits(t, a.ID)[1]
	ed, err := DecodeEdit(split)
	if err != nil {
		t.Fatal(err)
	}
	second := e.get(t, ed.ActivityID)
	if second.Source != "split" || *second.SplitOf != a.ID || second.Sport != "trail_running" {
		t.Errorf("second part: %+v", second)
	}
	first, rest := e.viewSamples(t, a.ID), e.viewSamples(t, second.ID)
	if len(first)+len(rest) != total || first[len(first)-1].Time.Compare(at) >= 0 || rest[0].Time.Before(at) {
		t.Errorf("split %d + %d of %d samples", len(first), len(rest), total)
	}
	if got := e.effective(t, a.ID); *got.ElapsedTimeS >= 1800 {
		t.Errorf("first part lasts %v s", *got.ElapsedTimeS)
	}
	if got := e.effective(t, second.ID); got.StartTime.Before(at) || got.DistanceM == nil || *got.DistanceM <= 0 {
		t.Errorf("second part: start %v, distance %v", got.StartTime, got.DistanceM)
	}

	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditSplit, At: &at}), "an activity that was merged or split cannot be split again")
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, second.ID, Edit{Kind: EditSplit, At: ptrTime(at.Add(time.Minute))}),
		"an activity that was merged or split cannot be split again")
	// The second part takes edits of its own.
	from, to := at.Add(time.Minute), at.Add(5*time.Minute)
	e.add(t, second.ID, Edit{Kind: EditTrim, From: &from, To: &to})
	if got := e.effective(t, second.ID); *got.ElapsedTimeS > 240 {
		t.Errorf("trimmed second part lasts %v s", *got.ElapsedTimeS)
	}

	if err := e.svc.UndoEdit(ctx, e.alice, a.ID, split.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.q.GetActivity(ctx, db.GetActivityParams{UserID: e.alice, ID: second.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("second part after undoing: %v", err)
	}
	if listed := e.activities(t, e.alice); len(listed) != 1 {
		t.Errorf("listed after undoing: %d", len(listed))
	}
	if got := e.effective(t, a.ID); !got.StartTime.Equal(a.StartTime) || *got.DistanceM != *a.DistanceM || got.Sport != "trail_running" {
		t.Errorf("after undoing the split: %+v", got)
	}
	if len(e.viewSamples(t, a.ID)) != total || e.streamHash(t, a.ID) != hash {
		t.Error("samples changed")
	}

	// A split point past the end leaves the second part empty: refused, nothing kept.
	end := a.StartTime.Add(24 * time.Hour)
	wantInvalid(t, e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditSplit, At: &end}), "the split must leave recorded samples on both sides")
	if listed := e.activities(t, e.alice); len(listed) != 1 {
		t.Errorf("a refused split was kept: %d listed", len(listed))
	}
}

func TestTrimClipsToSegments(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a := e.upload(t, "intervals.fit")
	at := a.StartTime.Add(30 * time.Minute)
	e.add(t, a.ID, Edit{Kind: EditSplit, At: &at})
	second := e.activities(t, e.alice)[0].ID
	// Trims reaching past a part's bounds stop at them.
	wide := Edit{Kind: EditTrim, From: ptrTime(a.StartTime.Add(-time.Hour)), To: ptrTime(a.StartTime.Add(10 * time.Hour))}
	e.add(t, a.ID, wide)
	e.add(t, second, wide)
	first, rest := e.viewSamples(t, a.ID), e.viewSamples(t, second)
	if first[len(first)-1].Time.Compare(at) >= 0 || rest[0].Time.Before(at) {
		t.Errorf("parts overlap after wide trims")
	}
}

func TestEditsOnBrokenData(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	a := e.upload(t, "intervals.fit")

	if _, err := Effective(db.Activity{Overrides: []byte("nope")}); err == nil {
		t.Error("Effective accepted broken overrides")
	}
	if _, err := DecodeEdit(db.ActivityEdit{Params: []byte("nope")}); err == nil {
		t.Error("DecodeEdit accepted broken params")
	}
	// An edit that cannot be read fails every read and write of the activity.
	bad, err := e.q.InsertActivityEdit(ctx, db.InsertActivityEditParams{UserID: e.alice, ActivityID: a.ID, Kind: EditTrim, Params: []byte(`{"from": 5}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.View(ctx, a); err == nil {
		t.Error("View read a broken edit")
	}
	if err := e.svc.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditSport, Sport: "cycling", Subtype: "outdoor"}); err == nil {
		t.Error("AddEdit read a broken edit")
	}
	if err := e.svc.UndoEdit(ctx, e.alice, a.ID, bad.ID); err == nil {
		t.Error("UndoEdit read a broken edit")
	}

	closed := pg.NewDB(t)
	closed.Close()
	broken := &Service{pool: closed, jobs: e.svc.jobs}
	if _, err := broken.View(ctx, a); err == nil {
		t.Error("View on a closed pool")
	}
	if _, err := broken.Segments(ctx, e.alice, View{Segments: []Segment{{ActivityID: a.ID, From: minTime, To: maxTime}}}, 10); err == nil {
		t.Error("segments read from a closed pool")
	}
	if _, err := broken.Samples(ctx, e.alice, View{Segments: []Segment{{ActivityID: a.ID, From: minTime, To: maxTime}}}, &a.StartTime, 10); err == nil {
		t.Error("Samples on a closed pool")
	}
	if err := broken.AddEdit(ctx, e.alice, a.ID, Edit{Kind: EditSport}); err == nil {
		t.Error("AddEdit on a closed pool")
	}
	if err := broken.UndoEdit(ctx, e.alice, a.ID, 1); err == nil {
		t.Error("UndoEdit on a closed pool")
	}
}
