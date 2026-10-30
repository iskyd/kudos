// Package ingest stores parsed activities: the gzipped original in the data
// directory, every row in one transaction, and a job to compute derived values.
package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/iskyd/kudos/internal/importer"
	"github.com/iskyd/kudos/internal/importer/fit"
	"github.com/iskyd/kudos/internal/importer/gpx"
	"github.com/iskyd/kudos/internal/importer/tcx"
	"github.com/iskyd/kudos/internal/jobs"
	"github.com/iskyd/kudos/internal/store/db"
)

// Errors callers branch on.
var (
	ErrUnsupportedFormat = errors.New("unsupported file format; upload a .fit, .gpx or .tcx file")
	ErrInvalidFile       = errors.New("the file could not be read")
	ErrNotFound          = errors.New("no such activity")
	ErrGearNotFound      = errors.New("no such gear")
)

// Service ingests activities.
type Service struct {
	pool    *pgxpool.Pool
	jobs    *jobs.Client
	dataDir string
}

// New returns a service writing originals under dataDir.
func New(pool *pgxpool.Pool, jobClient *jobs.Client, dataDir string) *Service {
	return &Service{pool: pool, jobs: jobClient, dataDir: dataDir}
}

// File is an original file as received.
type File struct {
	Format string // fit, gpx or tcx
	Data   []byte
	Origin Origin
}

// Origin is where a file came from. The zero value is a user upload, whose
// activities are keyed by start time, sport and device; a source keys them by
// its own activity ID (invariant 6).
type Origin struct {
	Source     string // "garmin"
	ActivityID string
}

// Upload decodes a file by its name's extension and ingests its activities.
// loc is the user's timezone, used when the file records no local offset.
func (s *Service) Upload(ctx context.Context, userID int64, loc *time.Location, name string, data []byte) ([]int64, error) {
	return s.Import(ctx, userID, loc, name, data, Origin{})
}

// Import is Upload for a file from a source. loc is the fallback for a file
// without a local offset.
func (s *Service) Import(ctx context.Context, userID int64, loc *time.Location, name string, data []byte, o Origin) ([]int64, error) {
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	var decode func(io.Reader) ([]importer.Activity, error)
	switch format {
	case "fit":
		decode = fit.Decode
	case "gpx":
		decode = gpx.Decode
	case "tcx":
		decode = tcx.Decode
	default:
		return nil, ErrUnsupportedFormat
	}
	acts, err := decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}
	return s.Ingest(ctx, userID, loc, File{Format: format, Data: data, Origin: o}, acts)
}

// Ingest stores an uploaded file's activities, replacing earlier imports of the
// same activities, and returns their IDs. On any error nothing is stored.
func (s *Service) Ingest(ctx context.Context, userID int64, loc *time.Location, f File, acts []importer.Activity) ([]int64, error) {
	sum := sha256.Sum256(f.Data)
	digest := hex.EncodeToString(sum[:])
	rel := filepath.Join("originals", fmt.Sprint(userID), digest+"."+f.Format+".gz")

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after commit; on failure the transaction is discarded anyway

	ids, created, err := s.ingestTx(ctx, tx, userID, loc, f, sum[:], rel, digest, acts)
	if err == nil {
		if cerr := tx.Commit(ctx); cerr != nil {
			err = fmt.Errorf("committing: %w", cerr)
		}
	}
	if err != nil {
		// Remove before the rollback releases the original_files row, so a concurrent
		// upload of the same file cannot see the file and then lose it.
		if created {
			_ = os.Remove(filepath.Join(s.dataDir, rel))
		}
		return nil, err
	}
	return ids, nil
}

func (s *Service) ingestTx(ctx context.Context, tx pgx.Tx, userID int64, loc *time.Location, f File,
	sum []byte, rel, digest string, acts []importer.Activity,
) (ids []int64, created bool, err error) {
	q := db.New(tx)
	// The row lock taken here serializes concurrent uploads of the same file.
	file, err := q.UpsertOriginalFile(ctx, db.UpsertOriginalFileParams{
		UserID: userID, Format: f.Format, Sha256: sum, SizeBytes: int64(len(f.Data)), Path: rel,
	})
	if err != nil {
		return nil, false, fmt.Errorf("storing original file row: %w", err)
	}
	if created, err = writeGzip(filepath.Join(s.dataDir, file.Path), f.Data); err != nil {
		return nil, false, err
	}

	var group *int64
	if len(acts) > 1 {
		source, groupID := "upload", digest
		if f.Origin.Source != "" {
			source, groupID = f.Origin.Source, f.Origin.ActivityID
		}
		id, err := q.UpsertMultisportGroup(ctx, db.UpsertMultisportGroupParams{UserID: userID, Source: source, SourceGroupID: groupID})
		if err != nil {
			return nil, created, fmt.Errorf("storing multisport group: %w", err)
		}
		group = &id
	}
	for i, a := range acts {
		source, key := "upload", sourceKey(a.StartTime, a.Sport, a.Device)
		if f.Origin.Source != "" {
			source, key = f.Origin.Source, f.Origin.ActivityID
			if len(acts) > 1 { // a multisport file holds its legs under one source ID
				key = fmt.Sprintf("%s/%d", key, i)
			}
		}
		id, err := upsertActivity(ctx, q, userID, loc, a, source, key, &file.ID, group)
		if err != nil {
			return nil, created, err
		}
		if _, err := s.jobs.InsertTx(ctx, tx, jobs.DerivedArgs{UserID: userID, ActivityID: id}, nil); err != nil {
			return nil, created, fmt.Errorf("enqueueing derived values: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, created, nil
}

// Manual is an activity entered without a file.
type Manual struct {
	Sport    importer.Sport
	Start    time.Time // in the activity's local timezone
	Duration float64   // s
	Distance *float64  // m
	RPE      *float64  // 0–10
	Notes    string
}

// CreateManual stores a manual activity and returns its ID. Submitting the same
// sport and start again updates that activity instead of adding another.
func (s *Service) CreateManual(ctx context.Context, userID int64, m Manual) (int64, error) {
	subtype := importer.SubtypeOutdoor
	if m.Sport == importer.SportPoolSwimming || m.Sport == importer.SportStrength {
		subtype = importer.SubtypeIndoor
	}
	_, off := m.Start.Zone()
	var id int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		id, err = db.New(tx).UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: userID, Source: "manual", SourceActivityID: sourceKey(m.Start, m.Sport, importer.Device{Manufacturer: "manual"}),
			Sport: string(m.Sport), Subtype: string(subtype), StartTime: m.Start.UTC(), UtcOffsetS: int32(off), //nolint:gosec // G115: zone offsets are within ±18 h
			ElapsedTimeS: &m.Duration, TimerTimeS: &m.Duration, DistanceM: m.Distance, Rpe: f32(m.RPE), Notes: text(m.Notes),
		})
		if err != nil {
			return fmt.Errorf("storing: %w", err)
		}
		if _, err := s.jobs.InsertTx(ctx, tx, jobs.DerivedArgs{UserID: userID, ActivityID: id}, nil); err != nil {
			return fmt.Errorf("enqueueing derived values: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("creating manual activity: %w", err)
	}
	return id, nil
}

// writeGzip writes data gzipped to path unless the file exists, and reports whether it wrote it.
func writeGzip(path string, data []byte) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("creating originals directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return false, fmt.Errorf("creating original file: %w", err)
	}
	defer os.Remove(tmp.Name()) // gone after the rename; removes leftovers on failure
	zw := gzip.NewWriter(tmp)
	_, err = zw.Write(data)
	if err == nil {
		err = zw.Close()
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return false, fmt.Errorf("writing original file: %w", err)
	}
	return true, nil
}

// sourceKey identifies an activity without a source ID by start time, sport and
// device, the spec's deduplication rule.
func sourceKey(start time.Time, sport importer.Sport, d importer.Device) string {
	return fmt.Sprintf("%s/%s/%s-%d-%d", start.UTC().Format(time.RFC3339), sport, d.Manufacturer, d.Product, d.Serial)
}

func upsertActivity(ctx context.Context, q *db.Queries, userID int64, loc *time.Location, a importer.Activity,
	source, key string, fileID, group *int64,
) (int64, error) {
	id, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
		UserID: userID, Source: source, SourceActivityID: key,
		Sport: string(a.Sport), Subtype: string(a.Subtype), StartTime: a.StartTime.UTC(), UtcOffsetS: offset(a, loc),
		OriginalFileID: fileID, MultisportGroupID: group,
		ElapsedTimeS: a.ElapsedTime, TimerTimeS: a.TimerTime, DistanceM: a.Distance,
		AscentM: f32(a.Ascent), DescentM: f32(a.Descent), AvgSpeedMS: f32(a.AvgSpeed), MaxSpeedMS: f32(a.MaxSpeed),
		AvgHrBpm: i16(a.AvgHR), MaxHrBpm: i16(a.MaxHR), AvgPowerW: i16(a.AvgPower), MaxPowerW: i16(a.MaxPower),
		AvgCadence: f32(a.AvgCadence), MaxCadence: f32(a.MaxCadence), AvgTemperatureC: f32(a.AvgTemperature),
		PoolLengthM: f32(a.PoolLength), CaloriesKcal: i32(a.Calories), Rpe: f32(a.RPE), Feel: i16(a.Feel),
	})
	if err != nil {
		return 0, fmt.Errorf("storing activity: %w", err)
	}
	if err := replaceChildren(ctx, q, userID, id, a); err != nil {
		return 0, err
	}
	if w := a.Workout; w != nil {
		steps, err := json.Marshal(w.Steps)
		if err != nil {
			return 0, fmt.Errorf("encoding workout: %w", err)
		}
		if err := q.UpsertRecordedWorkout(ctx, db.UpsertRecordedWorkoutParams{UserID: userID, ActivityID: id, Name: text(w.Name), Steps: steps}); err != nil {
			return 0, fmt.Errorf("storing workout: %w", err)
		}
	}
	if th := a.Thresholds; th != nil {
		if err := q.UpsertActivityThresholds(ctx, db.UpsertActivityThresholdsParams{
			UserID: userID, ActivityID: id, FtpW: i16(th.FTP), ThresholdHrBpm: i16(th.ThresholdHR),
			MaxHrBpm: i16(th.MaxHR), RestingHrBpm: i16(th.RestingHR), HrZonesBpm: zones(th.HRZones), PowerZonesW: zones(th.PowerZones),
		}); err != nil {
			return 0, fmt.Errorf("storing thresholds: %w", err)
		}
	}
	return id, nil
}

// replaceChildren deletes and re-inserts samples, laps, lengths and sets: the parse
// of the same activity is the same, so a re-import changes nothing.
func replaceChildren(ctx context.Context, q *db.Queries, userID, id int64, a importer.Activity) error {
	key := struct{ UserID, ActivityID int64 }{userID, id}
	if err := q.DeleteActivityStreams(ctx, db.DeleteActivityStreamsParams(key)); err != nil {
		return fmt.Errorf("deleting samples: %w", err)
	}
	if err := q.DeleteLaps(ctx, db.DeleteLapsParams(key)); err != nil {
		return fmt.Errorf("deleting laps: %w", err)
	}
	if err := q.DeleteSwimLengths(ctx, db.DeleteSwimLengthsParams(key)); err != nil {
		return fmt.Errorf("deleting lengths: %w", err)
	}
	if err := q.DeleteStrengthSets(ctx, db.DeleteStrengthSetsParams(key)); err != nil {
		return fmt.Errorf("deleting sets: %w", err)
	}

	samples := make([]db.InsertActivityStreamsParams, len(a.Samples))
	for i, s := range a.Samples {
		var extras []byte
		if len(s.Extras) > 0 {
			var err error
			if extras, err = json.Marshal(s.Extras); err != nil {
				return fmt.Errorf("encoding sample extras: %w", err)
			}
		}
		samples[i] = db.InsertActivityStreamsParams{
			UserID: userID, ActivityID: id, Time: s.Time, LatDeg: s.Lat, LonDeg: s.Lon, AltitudeM: f32(s.Altitude),
			HrBpm: i16(s.HR), PowerW: i16(s.Power), Cadence: f32(s.Cadence), SpeedMS: f32(s.Speed),
			DistanceM: s.Distance, TemperatureC: f32(s.Temperature), Extras: extras,
		}
	}
	if _, err := q.InsertActivityStreams(ctx, samples); err != nil {
		return fmt.Errorf("storing samples: %w", err)
	}

	laps := make([]db.InsertLapsParams, len(a.Laps))
	for i, l := range a.Laps {
		laps[i] = db.InsertLapsParams{
			UserID: userID, ActivityID: id, LapIndex: int32(i), StartTime: l.StartTime,
			ElapsedTimeS: l.ElapsedTime, TimerTimeS: l.TimerTime, DistanceM: l.Distance,
			AscentM: f32(l.Ascent), DescentM: f32(l.Descent), AvgSpeedMS: f32(l.AvgSpeed), MaxSpeedMS: f32(l.MaxSpeed),
			AvgHrBpm: i16(l.AvgHR), MaxHrBpm: i16(l.MaxHR), AvgPowerW: i16(l.AvgPower), MaxPowerW: i16(l.MaxPower),
			AvgCadence: f32(l.AvgCadence), Intensity: text(l.Intensity), WorkoutStepIndex: index(l.WorkoutStepIndex),
		}
	}
	if _, err := q.InsertLaps(ctx, laps); err != nil {
		return fmt.Errorf("storing laps: %w", err)
	}

	lengths := make([]db.InsertSwimLengthsParams, len(a.Lengths))
	for i, l := range a.Lengths {
		lengths[i] = db.InsertSwimLengthsParams{
			UserID: userID, ActivityID: id, LengthIndex: int32(i), StartTime: l.StartTime,
			ElapsedTimeS: l.ElapsedTime, TimerTimeS: l.TimerTime, Active: l.Active, Stroke: text(l.Stroke),
			Strokes: i16(l.Strokes), AvgSpeedMS: f32(l.AvgSpeed),
		}
	}
	if _, err := q.InsertSwimLengths(ctx, lengths); err != nil {
		return fmt.Errorf("storing lengths: %w", err)
	}

	sets := make([]db.InsertStrengthSetsParams, len(a.Sets))
	for i, st := range a.Sets {
		sets[i] = db.InsertStrengthSetsParams{
			UserID: userID, ActivityID: id, SetIndex: int32(i), StartTime: st.StartTime,
			DurationS: st.Duration, Active: st.Active, Reps: i16(st.Reps), WeightKg: f32(st.Weight),
			ExerciseCategory: index(st.ExerciseCategory), ExerciseNumber: index(st.ExerciseNumber),
			ExerciseName: text(st.ExerciseName), WorkoutStepIndex: index(st.WorkoutStepIndex),
		}
	}
	if _, err := q.InsertStrengthSets(ctx, sets); err != nil {
		return fmt.Errorf("storing sets: %w", err)
	}
	return nil
}

// offset is the file's local offset, or else the user's timezone offset at the start (invariant 9).
func offset(a importer.Activity, loc *time.Location) int32 {
	if a.UTCOffset != nil {
		return int32(a.UTCOffset.Seconds())
	}
	_, off := a.StartTime.In(loc).Zone()
	return int32(off) //nolint:gosec // G115: zone offsets are within ±18 h
}

func f32(v *float64) *float32 {
	if v == nil {
		return nil
	}
	f := float32(*v)
	return &f
}

// i16 rounds to a smallint, dropping values a smallint cannot hold.
func i16(v *float64) *int16 {
	if v == nil || math.Round(*v) < math.MinInt16 || math.Round(*v) > math.MaxInt16 {
		return nil
	}
	i := int16(math.Round(*v))
	return &i
}

// i32 rounds to an integer, dropping values an integer cannot hold.
func i32(v *float64) *int32 {
	if v == nil || math.Round(*v) < math.MinInt32 || math.Round(*v) > math.MaxInt32 {
		return nil
	}
	i := int32(math.Round(*v))
	return &i
}

func index(v *int) *int32 {
	if v == nil || *v < math.MinInt32 || *v > math.MaxInt32 {
		return nil
	}
	i := int32(*v)
	return &i
}

func text(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func zones(vs []float64) []int16 {
	var out []int16
	for _, v := range vs {
		if i := i16(&v); i != nil {
			out = append(out, *i)
		}
	}
	return out
}

// Annotation is what the user adds to an activity. Nil (or no tags) clears a field.
type Annotation struct {
	RPE                *float32 // 0–10
	Feel               *int16   // 0–100, the mood
	Notes              *string
	PainTags           []string
	WeatherTemperature *float32 // °C
	WeatherCondition   *string
	GearID             *int64
}

// Annotate replaces the activity's annotation and queues its derived values for
// recomputation, since session-RPE load depends on RPE (invariant 23).
func (s *Service) Annotate(ctx context.Context, userID, activityID int64, a Annotation) error {
	tags := a.PainTags
	if tags == nil {
		tags = []string{}
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		n, err := db.New(tx).UpdateActivityAnnotation(ctx, db.UpdateActivityAnnotationParams{
			UserID: userID, ID: activityID, Rpe: a.RPE, Feel: a.Feel, Notes: a.Notes, PainTags: tags,
			WeatherTemperatureC: a.WeatherTemperature, WeatherCondition: a.WeatherCondition, GearID: a.GearID,
		})
		// The composite foreign key rejects gear that is not the user's.
		if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.ConstraintName == "activities_gear_fk" {
			return ErrGearNotFound
		}
		if err != nil {
			return fmt.Errorf("storing: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		if _, err := s.jobs.InsertTx(ctx, tx, jobs.DerivedArgs{UserID: userID, ActivityID: activityID}, nil); err != nil {
			return fmt.Errorf("enqueueing derived values: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("annotating activity: %w", err)
	}
	return nil
}
