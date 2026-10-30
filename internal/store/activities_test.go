package store_test

import (
	"crypto/sha256"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

var start = time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

// activityKey matches every Delete*/Get* params struct keyed by user and activity.
type activityKey struct{ UserID, ActivityID int64 }

// importActivity writes an activity with every kind of child row, the way ingestion
// replaces an activity: upsert the parents, then delete and re-insert the children.
func importActivity(t *testing.T, q *db.Queries, userID int64, sourceID string, rpe *float32) int64 {
	t.Helper()
	ctx := t.Context()
	sum := sha256.Sum256([]byte(sourceID))
	file, err := q.UpsertOriginalFile(ctx, db.UpsertOriginalFileParams{
		UserID: userID, Format: "fit", Sha256: sum[:], SizeBytes: 1234, Path: "files/" + sourceID + ".fit.gz",
	})
	check(t, err)
	group, err := q.UpsertMultisportGroup(ctx, db.UpsertMultisportGroupParams{UserID: userID, Source: "garmin", SourceGroupID: "group-" + sourceID})
	check(t, err)
	id, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
		UserID: userID, Source: "garmin", SourceActivityID: sourceID, Sport: "running", Subtype: "outdoor",
		StartTime: start, UtcOffsetS: 7200, OriginalFileID: &file.ID, MultisportGroupID: &group,
		ElapsedTimeS: ptr(5400.5), TimerTimeS: ptr(5390.0), DistanceM: ptr(21097.5), AscentM: ptr[float32](45),
		AvgHrBpm: ptr[int16](165), MaxHrBpm: ptr[int16](182), AvgCadence: ptr[float32](178.5), CaloriesKcal: ptr[int32](1450), Rpe: rpe, Feel: ptr[int16](75),
	})
	check(t, err)
	key := activityKey{userID, id}

	check(t, q.DeleteActivityStreams(ctx, db.DeleteActivityStreamsParams(key)))
	streams := make([]db.InsertActivityStreamsParams, 3)
	for i := range streams {
		streams[i] = db.InsertActivityStreamsParams{
			UserID: userID, ActivityID: id, Time: start.Add(time.Duration(i) * time.Second),
			LatDeg: ptr(45.1 + float64(i)/1e4), LonDeg: ptr(-30.0), HrBpm: ptr(int16(140 + i)), DistanceM: ptr(float64(i) * 3.2),
			Extras: []byte(`{"vertical_oscillation_m": 0.085}`),
		}
	}
	check(t, second(q.InsertActivityStreams(ctx, streams)))

	check(t, q.DeleteLaps(ctx, db.DeleteLapsParams(key)))
	check(t, second(q.InsertLaps(ctx, []db.InsertLapsParams{
		{UserID: userID, ActivityID: id, LapIndex: 0, StartTime: start, DistanceM: ptr(400.0), Intensity: ptr("warmup"), WorkoutStepIndex: ptr[int32](0)},
		{UserID: userID, ActivityID: id, LapIndex: 1, StartTime: start.Add(time.Minute), DistanceM: ptr(400.0), Intensity: ptr("active"), WorkoutStepIndex: ptr[int32](1)},
	})))

	check(t, q.DeleteSwimLengths(ctx, db.DeleteSwimLengthsParams(key)))
	check(t, second(q.InsertSwimLengths(ctx, []db.InsertSwimLengthsParams{
		{UserID: userID, ActivityID: id, LengthIndex: 0, StartTime: start, TimerTimeS: ptr(52.1), Active: true, Stroke: ptr("freestyle"), Strokes: ptr[int16](40)},
		{UserID: userID, ActivityID: id, LengthIndex: 1, StartTime: start.Add(time.Minute), TimerTimeS: ptr(30.0), Active: false},
	})))

	check(t, q.DeleteStrengthSets(ctx, db.DeleteStrengthSetsParams(key)))
	check(t, second(q.InsertStrengthSets(ctx, []db.InsertStrengthSetsParams{
		{UserID: userID, ActivityID: id, SetIndex: 0, StartTime: start, DurationS: ptr(40.0), Active: true, Reps: ptr[int16](12),
			WeightKg: ptr[float32](0), ExerciseCategory: ptr[int32](28), ExerciseNumber: ptr[int32](3), ExerciseName: ptr("Plank")},
	})))

	check(t, q.UpsertRecordedWorkout(ctx, db.UpsertRecordedWorkoutParams{
		UserID: userID, ActivityID: id, Name: ptr("ripetute brevi"), Steps: []byte(`[{"duration_m": 2000}]`),
	}))
	check(t, q.UpsertActivityThresholds(ctx, db.UpsertActivityThresholdsParams{
		UserID: userID, ActivityID: id, FtpW: ptr[int16](250), ThresholdHrBpm: ptr[int16](172),
		HrZonesBpm: []int16{120, 140, 155, 170, 190}, PowerZonesW: []int16{140, 190, 230, 270, 330, 400},
	}))
	return id
}

// activitySnapshot is everything the read queries return for one activity.
type activitySnapshot struct {
	Activity   db.Activity
	File       db.OriginalFile
	Streams    []db.ActivityStream
	Laps       []db.Lap
	Lengths    []db.SwimLength
	Sets       []db.StrengthSet
	Workout    db.RecordedWorkout
	Thresholds db.ActivityThreshold
}

func snapshot(t *testing.T, q *db.Queries, userID, id int64) activitySnapshot {
	t.Helper()
	ctx := t.Context()
	key := activityKey{userID, id}
	var s activitySnapshot
	var err error
	s.Activity, err = q.GetActivity(ctx, db.GetActivityParams{UserID: userID, ID: id})
	check(t, err)
	s.File, err = q.GetOriginalFile(ctx, db.GetOriginalFileParams{UserID: userID, ID: *s.Activity.OriginalFileID})
	check(t, err)
	s.Streams, err = q.ListActivityStreams(ctx, db.ListActivityStreamsParams{UserID: userID, ActivityID: id, MaxRows: 100})
	check(t, err)
	s.Laps, err = q.ListLaps(ctx, db.ListLapsParams{UserID: userID, ActivityID: id, After: -1, MaxRows: 100})
	check(t, err)
	s.Lengths, err = q.ListSwimLengths(ctx, db.ListSwimLengthsParams{UserID: userID, ActivityID: id, After: -1, MaxRows: 100})
	check(t, err)
	s.Sets, err = q.ListStrengthSets(ctx, db.ListStrengthSetsParams{UserID: userID, ActivityID: id, After: -1, MaxRows: 100})
	check(t, err)
	s.Workout, err = q.GetRecordedWorkout(ctx, db.GetRecordedWorkoutParams(key))
	check(t, err)
	s.Thresholds, err = q.GetActivityThresholds(ctx, db.GetActivityThresholdsParams(key))
	check(t, err)
	return s
}

func TestImportActivityRoundTrip(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)

	s := snapshot(t, q, alice.ID, importActivity(t, q, alice.ID, "a1", ptr[float32](6.5)))

	a := s.Activity
	if a.Sport != "running" || a.Subtype != "outdoor" || !a.StartTime.Equal(start) || a.UtcOffsetS != 7200 ||
		*a.DistanceM != 21097.5 || *a.AvgHrBpm != 165 || *a.CaloriesKcal != 1450 || *a.Rpe != 6.5 || *a.Feel != 75 || a.Notes != nil || string(a.Overrides) != "{}" {
		t.Errorf("activity = %+v", a)
	}
	if s.File.Format != "fit" || s.File.SizeBytes != 1234 || len(s.File.Sha256) != 32 {
		t.Errorf("file = %+v", s.File)
	}
	if len(s.Streams) != 3 || *s.Streams[2].HrBpm != 142 || !s.Streams[2].Time.Equal(start.Add(2*time.Second)) || s.Streams[0].PowerW != nil {
		t.Errorf("streams = %+v", s.Streams)
	}
	if len(s.Laps) != 2 || *s.Laps[1].Intensity != "active" || *s.Laps[1].WorkoutStepIndex != 1 {
		t.Errorf("laps = %+v", s.Laps)
	}
	if len(s.Lengths) != 2 || !s.Lengths[0].Active || s.Lengths[1].Active || *s.Lengths[0].Strokes != 40 {
		t.Errorf("lengths = %+v", s.Lengths)
	}
	if len(s.Sets) != 1 || *s.Sets[0].ExerciseName != "Plank" || *s.Sets[0].Reps != 12 {
		t.Errorf("sets = %+v", s.Sets)
	}
	if *s.Workout.Name != "ripetute brevi" {
		t.Errorf("workout = %+v", s.Workout)
	}
	if *s.Thresholds.FtpW != 250 || !reflect.DeepEqual(s.Thresholds.HrZonesBpm, []int16{120, 140, 155, 170, 190}) || s.Thresholds.MaxHrBpm != nil {
		t.Errorf("thresholds = %+v", s.Thresholds)
	}
}

func TestReimportChangesNothing(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)

	id := importActivity(t, q, alice.ID, "a1", ptr[float32](6.5))
	before := snapshot(t, q, alice.ID, id)
	// The second run carries no RPE: the stored one belongs to the user now.
	if again := importActivity(t, q, alice.ID, "a1", nil); again != id {
		t.Fatalf("re-import returned id %d, want %d", again, id)
	}
	if after := snapshot(t, q, alice.ID, id); !reflect.DeepEqual(before, after) {
		t.Fatalf("re-import changed data:\nbefore %+v\nafter  %+v", before, after)
	}
	page, err := q.ListActivities(t.Context(), firstPage(alice.ID, 10))
	if err != nil || len(page) != 1 {
		t.Fatalf("ListActivities = %d rows, %v; want 1", len(page), err)
	}
}

func TestUpsertActivityUpdatesSourceFields(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	params := db.UpsertActivityParams{
		UserID: alice.ID, Source: "upload", SourceActivityID: "fit:1:2026-10-15T07:00:00Z:running",
		Sport: "running", Subtype: "outdoor", StartTime: start, DistanceM: ptr(1000.0), Rpe: ptr[float32](5),
	}
	id, err := q.UpsertActivity(ctx, params)
	check(t, err)
	params.Sport, params.Subtype, params.DistanceM, params.Rpe = "trail_running", "virtual", ptr(1200.0), ptr[float32](9)
	if again, err := q.UpsertActivity(ctx, params); err != nil || again != id {
		t.Fatalf("second UpsertActivity = %d, %v; want %d", again, err, id)
	}
	a, err := q.GetActivity(ctx, db.GetActivityParams{UserID: alice.ID, ID: id})
	check(t, err)
	if a.Sport != "trail_running" || a.Subtype != "virtual" || *a.DistanceM != 1200 || *a.Rpe != 5 {
		t.Fatalf("after update: sport %s, subtype %s, distance %v, rpe %v; want trail_running, virtual, 1200, 5 (kept)",
			a.Sport, a.Subtype, *a.DistanceM, *a.Rpe)
	}
}

func firstPage(userID int64, n int32) db.ListActivitiesParams {
	return db.ListActivitiesParams{UserID: userID, Sort: "date", BeforeKey: math.MaxFloat64, BeforeID: math.MaxInt64, MaxRows: n}
}

func TestListActivitiesPagination(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	// Two activities share a start time: the id breaks the tie.
	var ids []int64
	for i, s := range []time.Time{start, start.Add(time.Hour), start.Add(time.Hour)} {
		id, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: alice.ID, Source: "manual", SourceActivityID: string(rune('a' + i)), Sport: "hiking", Subtype: "outdoor", StartTime: s,
		})
		check(t, err)
		ids = append(ids, id)
	}

	var got []int64
	p := firstPage(alice.ID, 2)
	for {
		page, err := q.ListActivities(ctx, p)
		check(t, err)
		for _, r := range page {
			got = append(got, r.Activity.ID)
		}
		if len(page) < int(p.MaxRows) {
			break
		}
		last := page[len(page)-1]
		p.BeforeKey, p.BeforeID = last.SortKey, last.Activity.ID
	}
	if want := []int64{ids[2], ids[1], ids[0]}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pages = %v, want %v", got, want)
	}
}

func TestChildListsPaginate(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	id := importActivity(t, q, alice.ID, "a1", nil)

	streams, err := q.ListActivityStreams(ctx, db.ListActivityStreamsParams{UserID: alice.ID, ActivityID: id, After: start, MaxRows: 1})
	if err != nil || len(streams) != 1 || !streams[0].Time.Equal(start.Add(time.Second)) {
		t.Errorf("ListActivityStreams after first sample = %+v, %v", streams, err)
	}
	laps, err := q.ListLaps(ctx, db.ListLapsParams{UserID: alice.ID, ActivityID: id, After: 0, MaxRows: 1})
	if err != nil || len(laps) != 1 || laps[0].LapIndex != 1 {
		t.Errorf("ListLaps after lap 0 = %+v, %v", laps, err)
	}
	lengths, err := q.ListSwimLengths(ctx, db.ListSwimLengthsParams{UserID: alice.ID, ActivityID: id, After: -1, MaxRows: 1})
	if err != nil || len(lengths) != 1 || lengths[0].LengthIndex != 0 {
		t.Errorf("ListSwimLengths first page = %+v, %v", lengths, err)
	}
	sets, err := q.ListStrengthSets(ctx, db.ListStrengthSetsParams{UserID: alice.ID, ActivityID: id, After: 0, MaxRows: 10})
	if err != nil || len(sets) != 0 {
		t.Errorf("ListStrengthSets after the only set = %+v, %v", sets, err)
	}
}

// TestActivitiesIsolation proves bob can neither read nor modify alice's activity
// data through any activity query (invariant 3).
func TestActivitiesIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	id := importActivity(t, q, alice.ID, "a1", ptr[float32](6.5))
	before := snapshot(t, q, alice.ID, id)
	bobOnAlice := activityKey{bob.ID, id}

	t.Run("reads", func(t *testing.T) {
		for name, err := range map[string]error{
			"GetActivity":           second(q.GetActivity(ctx, db.GetActivityParams{UserID: bob.ID, ID: id})),
			"GetOriginalFile":       second(q.GetOriginalFile(ctx, db.GetOriginalFileParams{UserID: bob.ID, ID: before.File.ID})),
			"GetRecordedWorkout":    second(q.GetRecordedWorkout(ctx, db.GetRecordedWorkoutParams(bobOnAlice))),
			"GetActivityThresholds": second(q.GetActivityThresholds(ctx, db.GetActivityThresholdsParams(bobOnAlice))),
		} {
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Errorf("%s as bob: error = %v, want ErrNoRows", name, err)
			}
		}
		activities, err := q.ListActivities(ctx, firstPage(bob.ID, 10))
		check(t, err)
		streams, err := q.ListActivityStreams(ctx, db.ListActivityStreamsParams{UserID: bob.ID, ActivityID: id, MaxRows: 100})
		check(t, err)
		laps, err := q.ListLaps(ctx, db.ListLapsParams{UserID: bob.ID, ActivityID: id, After: -1, MaxRows: 100})
		check(t, err)
		lengths, err := q.ListSwimLengths(ctx, db.ListSwimLengthsParams{UserID: bob.ID, ActivityID: id, After: -1, MaxRows: 100})
		check(t, err)
		sets, err := q.ListStrengthSets(ctx, db.ListStrengthSetsParams{UserID: bob.ID, ActivityID: id, After: -1, MaxRows: 100})
		check(t, err)
		for name, n := range map[string]int{
			"ListActivities": len(activities), "ListActivityStreams": len(streams), "ListLaps": len(laps),
			"ListSwimLengths": len(lengths), "ListStrengthSets": len(sets),
		} {
			if n != 0 {
				t.Errorf("%s as bob returned %d of alice's rows", name, n)
			}
		}
	})

	t.Run("deletes", func(t *testing.T) {
		check(t, q.DeleteActivityStreams(ctx, db.DeleteActivityStreamsParams(bobOnAlice)))
		check(t, q.DeleteLaps(ctx, db.DeleteLapsParams(bobOnAlice)))
		check(t, q.DeleteSwimLengths(ctx, db.DeleteSwimLengthsParams(bobOnAlice)))
		check(t, q.DeleteStrengthSets(ctx, db.DeleteStrengthSetsParams(bobOnAlice)))
	})

	t.Run("inserts pointing at alice's rows fail", func(t *testing.T) {
		for name, err := range map[string]error{
			"InsertActivityStreams": second(q.InsertActivityStreams(ctx, []db.InsertActivityStreamsParams{{UserID: bob.ID, ActivityID: id, Time: start.Add(time.Hour)}})),
			"InsertLaps":            second(q.InsertLaps(ctx, []db.InsertLapsParams{{UserID: bob.ID, ActivityID: id, LapIndex: 9, StartTime: start}})),
			"InsertSwimLengths":     second(q.InsertSwimLengths(ctx, []db.InsertSwimLengthsParams{{UserID: bob.ID, ActivityID: id, LengthIndex: 9, StartTime: start}})),
			"InsertStrengthSets":    second(q.InsertStrengthSets(ctx, []db.InsertStrengthSetsParams{{UserID: bob.ID, ActivityID: id, SetIndex: 9, StartTime: start}})),
			"UpsertActivity with alice's file": second(q.UpsertActivity(ctx, db.UpsertActivityParams{
				UserID: bob.ID, Source: "upload", SourceActivityID: "b1", Sport: "running", Subtype: "outdoor", StartTime: start, OriginalFileID: &before.File.ID,
			})),
			"UpsertActivity with alice's group": second(q.UpsertActivity(ctx, db.UpsertActivityParams{
				UserID: bob.ID, Source: "upload", SourceActivityID: "b2", Sport: "running", Subtype: "outdoor", StartTime: start, MultisportGroupID: before.Activity.MultisportGroupID,
			})),
		} {
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
				t.Errorf("%s as bob: error = %v, want foreign key violation", name, err)
			}
		}
	})

	t.Run("upserts on alice's keys", func(t *testing.T) {
		check(t, q.UpsertRecordedWorkout(ctx, db.UpsertRecordedWorkoutParams{UserID: bob.ID, ActivityID: id, Name: ptr("bob"), Steps: []byte(`[]`)}))
		check(t, q.UpsertActivityThresholds(ctx, db.UpsertActivityThresholdsParams{UserID: bob.ID, ActivityID: id, FtpW: ptr[int16](999)}))

		// The same source keys and file hash give bob rows of his own.
		bobs := snapshot(t, q, bob.ID, importActivity(t, q, bob.ID, "a1", nil))
		if bobs.Activity.ID == id || bobs.File.ID == before.File.ID || *bobs.Activity.MultisportGroupID == *before.Activity.MultisportGroupID {
			t.Errorf("bob's import reused alice's rows: %+v", bobs.Activity)
		}
	})

	if after := snapshot(t, q, alice.ID, id); !reflect.DeepEqual(before, after) {
		t.Fatalf("bob changed alice's data:\nbefore %+v\nafter  %+v", before, after)
	}
}

func second[T any](_ T, err error) error { return err }

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// listIDs pages through every activity matching p, two at a time.
func listIDs(t *testing.T, q *db.Queries, p db.ListActivitiesParams) []string {
	t.Helper()
	p.BeforeKey, p.BeforeID, p.MaxRows = math.MaxFloat64, math.MaxInt64, 2
	var got []string
	for {
		page, err := q.ListActivities(t.Context(), p)
		check(t, err)
		for _, r := range page {
			got = append(got, r.Activity.SourceActivityID)
		}
		if len(page) < int(p.MaxRows) {
			return got
		}
		last := page[len(page)-1]
		p.BeforeKey, p.BeforeID = last.SortKey, last.Activity.ID
	}
}

func TestListActivitiesFiltersAndSorts(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	// "late" starts 23:30 UTC on 14 Oct, which is 15 Oct where it happened (+2 h).
	for _, a := range []struct {
		id, sport string
		start     time.Time
		offset    int32
		distance  *float64
		timer     *float64
		elapsed   *float64
	}{
		{"run", "running", time.Date(2026, 10, 10, 7, 0, 0, 0, time.UTC), 7200, ptr(10000.0), ptr(3000.0), ptr(3100.0)},
		{"ride", "cycling", time.Date(2026, 10, 12, 7, 0, 0, 0, time.UTC), 7200, ptr(40000.0), ptr(5400.0), nil},
		{"late", "running", time.Date(2026, 10, 14, 23, 30, 0, 0, time.UTC), 7200, ptr(5000.0), nil, ptr(1800.0)},
		{"lift", "strength", time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC), 7200, nil, ptr(2400.0), nil},
		{"walk", "hiking", time.Date(2026, 10, 18, 7, 0, 0, 0, time.UTC), 7200, nil, nil, nil},
	} {
		_, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: alice.ID, Source: "manual", SourceActivityID: a.id, Sport: a.sport, Subtype: "outdoor",
			StartTime: a.start, UtcOffsetS: a.offset, DistanceM: a.distance, TimerTimeS: a.timer, ElapsedTimeS: a.elapsed,
		})
		check(t, err)
	}
	// Bob's activity matches every filter and never shows.
	_, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
		UserID: bob.ID, Source: "manual", SourceActivityID: "bob", Sport: "running", Subtype: "outdoor",
		StartTime: time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC), DistanceM: ptr(99000.0),
	})
	check(t, err)

	running := "running"
	tests := []struct {
		name string
		p    db.ListActivitiesParams
		want []string
	}{
		{"by date", db.ListActivitiesParams{Sort: "date"}, []string{"walk", "lift", "late", "ride", "run"}},
		{"unknown sort is by date", db.ListActivitiesParams{Sort: ""}, []string{"walk", "lift", "late", "ride", "run"}},
		{"by distance, missing last", db.ListActivitiesParams{Sort: "distance"}, []string{"ride", "run", "late", "walk", "lift"}},
		// Timer time, else elapsed time, else last.
		{"by duration", db.ListActivitiesParams{Sort: "duration"}, []string{"ride", "run", "lift", "late", "walk"}},
		{"sport", db.ListActivitiesParams{Sort: "date", Sport: &running}, []string{"late", "run"}},
		{"from a local day", db.ListActivitiesParams{Sort: "date", FromDay: ptr(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))}, []string{"walk", "lift", "late"}},
		{"to a local day", db.ListActivitiesParams{Sort: "date", ToDay: ptr(time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))}, []string{"ride", "run"}},
		{"one day", db.ListActivitiesParams{
			Sort: "distance", FromDay: ptr(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)), ToDay: ptr(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)),
		}, []string{"late"}},
		{"nothing matches", db.ListActivitiesParams{Sort: "date", FromDay: ptr(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))}, nil},
	}
	for _, tt := range tests {
		tt.p.UserID = alice.ID
		if got := listIDs(t, q, tt.p); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestListMultisportLegsIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	group, err := q.UpsertMultisportGroup(ctx, db.UpsertMultisportGroupParams{UserID: alice.ID, Source: "upload", SourceGroupID: "tri"})
	check(t, err)
	for i, sport := range []string{"cycling", "open_water_swimming"} {
		// Inserted out of order: legs come back by start time.
		_, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: alice.ID, Source: "upload", SourceActivityID: sport, Sport: sport, Subtype: "outdoor",
			StartTime: start.Add(time.Duration(1-i) * time.Hour), MultisportGroupID: &group,
		})
		check(t, err)
	}
	legs, err := q.ListMultisportLegs(ctx, db.ListMultisportLegsParams{UserID: alice.ID, MultisportGroupID: &group})
	check(t, err)
	if len(legs) != 2 || legs[0].Sport != "open_water_swimming" || legs[1].Sport != "cycling" {
		t.Errorf("legs = %v", legs)
	}
	if legs, err := q.ListMultisportLegs(ctx, db.ListMultisportLegsParams{UserID: bob.ID, MultisportGroupID: &group}); err != nil || len(legs) != 0 {
		t.Errorf("bob lists %d of alice's legs, %v", len(legs), err)
	}
}

func TestActivityAnnotationAndTagFilter(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	ids := map[string]int64{}
	for i, name := range []string{"easy", "long", "tempo"} {
		id, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: alice.ID, Source: "manual", SourceActivityID: name, Sport: "running", Subtype: "outdoor",
			StartTime: start.Add(time.Duration(i) * time.Hour),
		})
		check(t, err)
		ids[name] = id
	}
	for name, tags := range map[string][]string{"long": {"left knee", "achilles"}, "tempo": {"left knee"}} {
		n, err := q.UpdateActivityAnnotation(ctx, db.UpdateActivityAnnotationParams{UserID: alice.ID, ID: ids[name], PainTags: tags})
		if err != nil || n != 1 {
			t.Fatalf("annotating %s: %d, %v", name, n, err)
		}
	}
	// Bob cannot annotate alice's activity.
	if n, err := q.UpdateActivityAnnotation(ctx, db.UpdateActivityAnnotationParams{UserID: bob.ID, ID: ids["easy"], PainTags: []string{"x"}}); err != nil || n != 0 {
		t.Errorf("bob annotated %d rows, %v", n, err)
	}

	for tag, want := range map[string][]string{"left knee": {"tempo", "long"}, "achilles": {"long"}, "back": nil} {
		if got := listIDs(t, q, db.ListActivitiesParams{UserID: alice.ID, Sort: "date", Tag: &tag}); !reflect.DeepEqual(got, want) {
			t.Errorf("tag %q: %v, want %v", tag, got, want)
		}
	}
	if got := listIDs(t, q, db.ListActivitiesParams{UserID: bob.ID, Sort: "date", Tag: ptr("left knee")}); got != nil {
		t.Errorf("bob lists %v", got)
	}
}
