package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"

	"github.com/iskyd/kudos/internal/importer"
	"github.com/iskyd/kudos/internal/importer/fit"
	"github.com/iskyd/kudos/internal/jobs"
	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

var pg *testutil.Postgres

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if pg, err = testutil.StartPostgres(ctx); err != nil {
		log.Fatal(err)
	}
	code := m.Run()
	if err := pg.Terminate(ctx); err != nil {
		log.Print(err)
	}
	os.Exit(code)
}

type env struct {
	pool       *pgxpool.Pool
	svc        *Service
	q          *db.Queries
	dir        string
	alice, bob int64
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := pg.NewDB(t)
	client, err := jobs.NewClient(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	alice, bob := testutil.TwoUsers(t, pool)
	dir := t.TempDir()
	return env{pool: pool, svc: New(pool, client, dir), q: db.New(pool), dir: dir, alice: alice.ID, bob: bob.ID}
}

func corpus(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../../../testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (e env) activities(t *testing.T, userID int64) []db.Activity {
	t.Helper()
	acts, err := e.q.ListActivities(t.Context(), db.ListActivitiesParams{
		UserID: userID, BeforeStart: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), BeforeID: math.MaxInt64, MaxRows: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	return acts
}

func (e env) sampleCount(t *testing.T, userID, activityID int64) int {
	t.Helper()
	rows, err := e.q.ListActivityStreams(t.Context(), db.ListActivityStreamsParams{
		UserID: userID, ActivityID: activityID, After: time.Time{}, MaxRows: 100000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

func gunzip(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestUploadTwiceYieldsOneActivity(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	data := corpus(t, "fit/intervals.fit")

	first, err := e.svc.Upload(ctx, e.alice, time.UTC, "Intervals.FIT", data)
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.svc.Upload(ctx, e.alice, time.UTC, "intervals.fit", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 || first[0] != second[0] {
		t.Fatalf("ids = %v then %v, want the same single id", first, second)
	}
	acts := e.activities(t, e.alice)
	if len(acts) != 1 {
		t.Fatalf("%d activities, want 1", len(acts))
	}
	a := acts[0]
	if a.Sport != "running" || a.Source != "upload" || a.UtcOffsetS != 7200 || a.OriginalFileID == nil {
		t.Errorf("activity = %s %s offset %d file %v", a.Sport, a.Source, a.UtcOffsetS, a.OriginalFileID)
	}
	if n := e.sampleCount(t, e.alice, a.ID); n != 2833 {
		t.Errorf("%d samples, want 2833 (not duplicated)", n)
	}
	laps, err := e.q.ListLaps(ctx, db.ListLapsParams{UserID: e.alice, ActivityID: a.ID, After: -1, MaxRows: 1000})
	if err != nil || len(laps) != 24 {
		t.Errorf("%d laps (%v), want 24", len(laps), err)
	}
	if _, err := e.q.GetRecordedWorkout(ctx, db.GetRecordedWorkoutParams{UserID: e.alice, ActivityID: a.ID}); err != nil {
		t.Errorf("recorded workout: %v", err)
	}
	if _, err := e.q.GetActivityThresholds(ctx, db.GetActivityThresholdsParams{UserID: e.alice, ActivityID: a.ID}); err != nil {
		t.Errorf("thresholds: %v", err)
	}

	// The original is kept gzipped and reads back byte for byte.
	file, err := e.q.GetOriginalFile(ctx, db.GetOriginalFileParams{UserID: e.alice, ID: *a.OriginalFileID})
	if err != nil {
		t.Fatal(err)
	}
	if file.Format != "fit" || file.SizeBytes != int64(len(data)) {
		t.Errorf("original file row = %+v", file)
	}
	if got := gunzip(t, filepath.Join(e.dir, file.Path)); !bytes.Equal(got, data) {
		t.Error("stored original differs from the upload")
	}
	// Each import queues a recompute of the derived values.
	want := jobs.DerivedArgs{UserID: e.alice, ActivityID: a.ID}
	rivertest.RequireManyInserted(ctx, t, riverpgxv5.New(e.pool), []rivertest.ExpectedJob{{Args: want}, {Args: want}})
}

func TestFailureRollsBackDatabaseAndFile(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	bad := []importer.Activity{{
		Sport: importer.SportRunning, Subtype: importer.SubtypeOutdoor,
		StartTime: time.Date(2026, 5, 1, 6, 0, 0, 0, time.UTC),
		Summary:   importer.Summary{Distance: ptr(-1.0)}, // violates distance_m >= 0
	}}
	data := []byte("original bytes")
	if _, err := e.svc.Ingest(ctx, e.alice, time.UTC, File{Format: "gpx", Data: data}, bad); err == nil {
		t.Fatal("invalid activity stored")
	}
	if acts := e.activities(t, e.alice); len(acts) != 0 {
		t.Errorf("%d activities after a failed ingest", len(acts))
	}
	if files := originals(t, e.dir); len(files) != 0 {
		t.Errorf("original files left behind: %v", files)
	}

	// A failed re-import of a stored file keeps the file the first import wrote.
	good := corpus(t, "gpx/run.gpx")
	if _, err := e.svc.Upload(ctx, e.alice, time.UTC, "run.gpx", good); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Ingest(ctx, e.alice, time.UTC, File{Format: "gpx", Data: good}, bad); err == nil {
		t.Fatal("invalid activity stored")
	}
	if files := originals(t, e.dir); len(files) != 1 {
		t.Errorf("originals = %v, want the first import's file", files)
	}
	if acts := e.activities(t, e.alice); len(acts) != 1 {
		t.Errorf("%d activities, want the first import's", len(acts))
	}
}

// Pool lengths, strength sets and developer-field extras are stored as parsed.
func TestUploadSportSpecificRows(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	for _, file := range []string{"pool_swim.fit", "strength.fit", "zwift.fit"} {
		data := corpus(t, "fit/"+file)
		parsed, err := fit.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		ids, err := e.svc.Upload(ctx, e.alice, time.UTC, file, data)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		lengths, err := e.q.ListSwimLengths(ctx, db.ListSwimLengthsParams{UserID: e.alice, ActivityID: ids[0], After: -1, MaxRows: 1000})
		if err != nil || len(lengths) != len(parsed[0].Lengths) {
			t.Errorf("%s: %d lengths (%v), want %d", file, len(lengths), err, len(parsed[0].Lengths))
		}
		sets, err := e.q.ListStrengthSets(ctx, db.ListStrengthSetsParams{UserID: e.alice, ActivityID: ids[0], After: -1, MaxRows: 1000})
		if err != nil || len(sets) != len(parsed[0].Sets) {
			t.Errorf("%s: %d sets (%v), want %d", file, len(sets), err, len(parsed[0].Sets))
		}
		samples, err := e.q.ListActivityStreams(ctx, db.ListActivityStreamsParams{UserID: e.alice, ActivityID: ids[0], MaxRows: 1})
		if err != nil || len(samples) != 1 {
			t.Fatalf("%s: samples %v", file, err)
		}
		if (samples[0].Extras != nil) != (len(parsed[0].Samples[0].Extras) > 0) {
			t.Errorf("%s: extras = %s, parsed %v", file, samples[0].Extras, parsed[0].Samples[0].Extras)
		}
	}
}

// A child row that cannot be stored rolls back the whole import.
func TestChildRowFailuresRollBack(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	start := time.Date(2026, 5, 1, 6, 0, 0, 0, time.UTC)
	for name, change := range map[string]func(*importer.Activity){
		"lap intensity": func(a *importer.Activity) { a.Laps = []importer.Lap{{StartTime: start, Intensity: "sprint"}} },
		"length stroke": func(a *importer.Activity) { a.Lengths = []importer.Length{{StartTime: start, Stroke: "doggy"}} },
		"sample time":   func(a *importer.Activity) { a.Samples = []importer.Sample{{Time: start}, {Time: start}} },
		"NaN extra": func(a *importer.Activity) {
			a.Samples = []importer.Sample{{Time: start, Extras: map[string]float64{"x": math.NaN()}}}
		},
		"NaN workout": func(a *importer.Activity) {
			a.Workout = &importer.Workout{Steps: []importer.WorkoutStep{{Duration: ptr(math.NaN())}}}
		},
	} {
		a := importer.Activity{Sport: importer.SportPoolSwimming, Subtype: importer.SubtypeIndoor, StartTime: start}
		change(&a)
		if _, err := e.svc.Ingest(t.Context(), e.alice, time.UTC, File{Format: "fit", Data: []byte(name)}, []importer.Activity{a}); err == nil {
			t.Errorf("%s: stored", name)
		}
	}
	if acts := e.activities(t, e.alice); len(acts) != 0 {
		t.Errorf("%d activities stored", len(acts))
	}
	if files := originals(t, e.dir); len(files) != 0 {
		t.Errorf("originals left behind: %v", files)
	}
}

func originals(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestUserIsolation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	data := corpus(t, "tcx/run.tcx")
	a, err := e.svc.Upload(ctx, e.alice, time.UTC, "run.tcx", data)
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.svc.Upload(ctx, e.bob, time.UTC, "run.tcx", data)
	if err != nil {
		t.Fatal(err)
	}
	if a[0] == b[0] {
		t.Fatal("bob's upload reused alice's activity")
	}
	if n := len(e.activities(t, e.alice)); n != 1 {
		t.Errorf("alice has %d activities, want 1", n)
	}
	if n := e.sampleCount(t, e.bob, a[0]); n != 0 {
		t.Errorf("bob reads %d of alice's samples", n)
	}
	if files := originals(t, e.dir); len(files) != 2 {
		t.Errorf("originals = %v, want one per user", files)
	}
}

func TestUploadMultisportAndOffsetFallback(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 5, 1, 6, 0, 0, 0, time.UTC)
	legs := []importer.Activity{
		{Sport: importer.SportOpenWaterSwimming, Subtype: importer.SubtypeOutdoor, StartTime: start},
		{Sport: importer.SportCycling, Subtype: importer.SubtypeOutdoor, StartTime: start.Add(time.Hour), UTCOffset: ptr(time.Hour)},
	}
	ids, err := e.svc.Ingest(ctx, e.alice, rome, File{Format: "fit", Data: []byte("triathlon")}, legs)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("ids = %v, want two activities", ids)
	}
	swim, err := e.q.GetActivity(ctx, db.GetActivityParams{UserID: e.alice, ID: ids[0]})
	if err != nil {
		t.Fatal(err)
	}
	bike, err := e.q.GetActivity(ctx, db.GetActivityParams{UserID: e.alice, ID: ids[1]})
	if err != nil {
		t.Fatal(err)
	}
	if swim.MultisportGroupID == nil || bike.MultisportGroupID == nil || *swim.MultisportGroupID != *bike.MultisportGroupID {
		t.Errorf("groups = %v, %v, want one shared group", swim.MultisportGroupID, bike.MultisportGroupID)
	}
	// No offset in the file: Rome in summer is +2 h. The file's own offset wins.
	if swim.UtcOffsetS != 7200 || bike.UtcOffsetS != 3600 {
		t.Errorf("offsets = %d, %d, want 7200, 3600", swim.UtcOffsetS, bike.UtcOffsetS)
	}
}

func TestUploadErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	if _, err := e.svc.Upload(ctx, e.alice, time.UTC, "notes.txt", []byte("hi")); !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("txt error = %v, want ErrUnsupportedFormat", err)
	}
	if _, err := e.svc.Upload(ctx, e.alice, time.UTC, "run.fit", []byte("not a fit file")); !errors.Is(err, ErrInvalidFile) {
		t.Errorf("garbage error = %v, want ErrInvalidFile", err)
	}

	// An unwritable data directory fails the ingest and stores nothing.
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e.svc.dataDir = blocked
	if _, err := e.svc.Upload(ctx, e.alice, time.UTC, "run.gpx", corpus(t, "gpx/run.gpx")); err == nil {
		t.Error("ingest succeeded without a writable data directory")
	}
	if acts := e.activities(t, e.alice); len(acts) != 0 {
		t.Errorf("%d activities stored", len(acts))
	}
}

func TestCreateManual(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	m := Manual{
		Sport: importer.SportStrength, Start: time.Date(2026, 1, 10, 18, 30, 0, 0, rome),
		Duration: 2700, RPE: ptr(7.0), Notes: "Gym",
	}
	id, err := e.svc.CreateManual(ctx, e.alice, m)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.svc.CreateManual(ctx, e.alice, m)
	if err != nil {
		t.Fatal(err)
	}
	if again != id {
		t.Errorf("resubmitting created %d, want %d", again, id)
	}
	a, err := e.q.GetActivity(ctx, db.GetActivityParams{UserID: e.alice, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if a.Source != "manual" || a.Sport != "strength" || a.Subtype != "indoor" || a.UtcOffsetS != 3600 ||
		!a.StartTime.Equal(time.Date(2026, 1, 10, 17, 30, 0, 0, time.UTC)) {
		t.Errorf("activity = %s %s/%s offset %d start %v", a.Source, a.Sport, a.Subtype, a.UtcOffsetS, a.StartTime)
	}
	if *a.ElapsedTimeS != 2700 || *a.TimerTimeS != 2700 || a.DistanceM != nil || *a.Rpe != 7 || *a.Notes != "Gym" {
		t.Errorf("values = %v %v %v %v %v", *a.ElapsedTimeS, *a.TimerTimeS, a.DistanceM, *a.Rpe, *a.Notes)
	}
	run, err := e.svc.CreateManual(ctx, e.bob, Manual{Sport: importer.SportRunning, Start: m.Start, Duration: 1800, Distance: ptr(5000.0)})
	if err != nil {
		t.Fatal(err)
	}
	if run == id {
		t.Error("bob's manual activity reused alice's")
	}
	if b, err := e.q.GetActivity(ctx, db.GetActivityParams{UserID: e.bob, ID: run}); err != nil || b.Subtype != "outdoor" || *b.DistanceM != 5000 {
		t.Errorf("bob's run = %+v, %v", b, err)
	}

	// A failure (here, an RPE out of range) stores nothing.
	if _, err := e.svc.CreateManual(ctx, e.alice, Manual{Sport: importer.SportRunning, Start: m.Start, Duration: 60, RPE: ptr(11.0)}); err == nil {
		t.Error("RPE 11 stored")
	}
}

func TestConversions(t *testing.T) {
	t.Parallel()
	if f32(nil) != nil || i16(nil) != nil || i32(nil) != nil || index(nil) != nil || text("") != nil {
		t.Error("nil or empty input converted to a value")
	}
	if v := i16(ptr(171.5)); v == nil || *v != 172 {
		t.Errorf("i16(171.5) = %v, want 172", v)
	}
	if i16(ptr(65535.0)) != nil || i16(ptr(-40000.0)) != nil {
		t.Error("i16 kept a value a smallint cannot hold")
	}
	if v := i32(ptr(1467.4)); v == nil || *v != 1467 {
		t.Errorf("i32(1467.4) = %v", v)
	}
	if i32(ptr(1e12)) != nil || index(ptr(math.MaxInt64)) != nil {
		t.Error("value out of int32 range kept")
	}
	if v := index(ptr(3)); v == nil || *v != 3 {
		t.Errorf("index(3) = %v", v)
	}
	if got := zones([]float64{120, 150, 70000}); len(got) != 2 || got[0] != 120 || got[1] != 150 {
		t.Errorf("zones = %v", got)
	}
	if v := f32(ptr(3.5)); v == nil || *v != 3.5 {
		t.Errorf("f32(3.5) = %v", v)
	}
	if got := sourceKey(time.Date(2026, 5, 1, 8, 0, 0, 0, time.FixedZone("", 7200)), importer.SportRunning,
		importer.Device{Manufacturer: "garmin", Product: 3990, Serial: 1}); got != "2026-05-01T06:00:00Z/running/garmin-3990-1" {
		t.Errorf("sourceKey = %q", got)
	}
}

func ptr[T any](v T) *T { return &v }

func TestImportKeysBySourceID(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	data := corpus(t, "gpx/run.gpx")
	garmin := Origin{Source: "garmin", ActivityID: "100001"}

	first, err := e.svc.Import(ctx, e.alice, time.UTC, "100001_ACTIVITY.gpx", data, garmin)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.svc.Import(ctx, e.alice, time.UTC, "100001_ACTIVITY.gpx", data, garmin)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(again) != 1 || first[0] != again[0] {
		t.Fatalf("ids = %v then %v, want the same activity", first, again)
	}
	a, err := e.q.GetActivity(ctx, db.GetActivityParams{UserID: e.alice, ID: first[0]})
	if err != nil {
		t.Fatal(err)
	}
	if a.Source != "garmin" || a.SourceActivityID != "100001" {
		t.Errorf("activity source = %s/%s, want garmin/100001", a.Source, a.SourceActivityID)
	}

	// A multisport file keys its legs under the one Garmin ID.
	start := time.Date(2026, 5, 1, 6, 0, 0, 0, time.UTC)
	legs := []importer.Activity{
		{Sport: importer.SportOpenWaterSwimming, Subtype: importer.SubtypeOutdoor, StartTime: start},
		{Sport: importer.SportCycling, Subtype: importer.SubtypeOutdoor, StartTime: start.Add(time.Hour)},
	}
	ids, err := e.svc.Ingest(ctx, e.alice, time.UTC, File{Format: "fit", Data: []byte("tri"), Origin: Origin{Source: "garmin", ActivityID: "7"}}, legs)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		a, err := e.q.GetActivity(ctx, db.GetActivityParams{UserID: e.alice, ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf("7/%d", i); a.Source != "garmin" || a.SourceActivityID != want || a.MultisportGroupID == nil {
			t.Errorf("leg %d = %s/%s group %v, want garmin/%s in a group", i, a.Source, a.SourceActivityID, a.MultisportGroupID, want)
		}
	}
}
