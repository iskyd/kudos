package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/importer"
	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/store/db"
)

const (
	// streamPageSize is how many full-resolution samples one page holds.
	streamPageSize = 5000
	// maxDecimatedSamples bounds what is read for a decimated response: 55 hours at 1 s.
	// shortcut: longer activities are decimated from their first 200 000 samples; page the read when one exists.
	maxDecimatedSamples = 200_000
)

var errActivityNotFound = &Error{Kind: KindNotFound, Detail: "no such activity"}

// activity returns the user's activity, or not-found for any other.
func (s *Server) activity(ctx context.Context, id int64) (db.Activity, error) {
	a, err := s.q.GetActivity(ctx, db.GetActivityParams{UserID: sessionFrom(ctx).userID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Activity{}, errActivityNotFound
	}
	if err != nil {
		return db.Activity{}, fmt.Errorf("reading activity: %w", err)
	}
	return a, nil
}

// GetActivity returns an activity with its recorded workout and multisport legs.
func (s *Server) GetActivity(ctx context.Context, req GetActivityRequestObject) (GetActivityResponseObject, error) {
	a, err := s.activity(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out, err := s.detail(ctx, a)
	if err != nil {
		return nil, err
	}
	return GetActivity200JSONResponse(out), nil
}

// detail adds the recorded workout and multisport legs to an activity.
func (s *Server) detail(ctx context.Context, a db.Activity) (ActivityDetail, error) {
	userID := sessionFrom(ctx).userID
	sum := activitySummary(a)
	out := ActivityDetail{
		Id: sum.Id, Sport: sum.Sport, Subtype: ActivityDetailSubtype(sum.Subtype), Source: ActivityDetailSource(sum.Source), StartTime: sum.StartTime, UtcOffsetS: sum.UtcOffsetS,
		ElapsedTimeS: sum.ElapsedTimeS, TimerTimeS: sum.TimerTimeS, DistanceM: sum.DistanceM, AscentM: sum.AscentM,
		AvgSpeedMS: sum.AvgSpeedMS, AvgHrBpm: sum.AvgHrBpm, AvgPowerW: sum.AvgPowerW, Rpe: sum.Rpe,
		DescentM: a.DescentM, MaxSpeedMS: a.MaxSpeedMS, MaxHrBpm: intPtr(a.MaxHrBpm), MaxPowerW: intPtr(a.MaxPowerW),
		AvgCadence: a.AvgCadence, MaxCadence: a.MaxCadence, AvgTemperatureC: a.AvgTemperatureC, PoolLengthM: a.PoolLengthM,
		CaloriesKcal: int32Ptr(a.CaloriesKcal), Feel: intPtr(a.Feel), Notes: a.Notes, WeatherTemperatureC: a.WeatherTemperatureC,
		WeatherCondition: (*ActivityDetailWeatherCondition)(a.WeatherCondition), Legs: []ActivitySummary{}, PainTags: a.PainTags,
		GearId: a.GearID,
	}

	w, err := s.q.GetRecordedWorkout(ctx, db.GetRecordedWorkoutParams{UserID: userID, ActivityID: a.ID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return ActivityDetail{}, fmt.Errorf("reading the recorded workout: %w", err)
	default:
		if out.Workout, err = recordedWorkout(w); err != nil {
			return ActivityDetail{}, err
		}
	}

	if a.MultisportGroupID != nil {
		legs, err := s.q.ListMultisportLegs(ctx, db.ListMultisportLegsParams{UserID: userID, MultisportGroupID: a.MultisportGroupID})
		if err != nil {
			return ActivityDetail{}, fmt.Errorf("listing multisport legs: %w", err)
		}
		for _, l := range legs {
			out.Legs = append(out.Legs, activitySummary(l))
		}
	}
	return out, nil
}

// Keep in step with ActivityAnnotation in api/openapi.yaml.
const (
	maxPainTags   = 10
	maxPainTagLen = 40
)

// SetActivityAnnotation replaces what the user added to an activity.
func (s *Server) SetActivityAnnotation(ctx context.Context, req SetActivityAnnotationRequestObject,
) (SetActivityAnnotationResponseObject, error) {
	b := req.Body
	invalid := func(detail string) (SetActivityAnnotationResponseObject, error) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: detail}
	}
	if b.Rpe != nil && !(*b.Rpe >= 0 && *b.Rpe <= 10) {
		return invalid("rpe must be 0 to 10")
	}
	if b.Feel != nil && (*b.Feel < 0 || *b.Feel > 100) {
		return invalid("feel must be 0 to 100")
	}
	if b.Notes != nil && utf8.RuneCountInString(*b.Notes) > maxNotesLen {
		return invalid(fmt.Sprintf("notes are limited to %d characters", maxNotesLen))
	}
	if b.WeatherTemperatureC != nil && !(*b.WeatherTemperatureC >= -60 && *b.WeatherTemperatureC <= 60) {
		return invalid("weatherTemperatureC must be -60 to 60")
	}
	if b.WeatherCondition != nil && !b.WeatherCondition.Valid() {
		return invalid("unknown weather condition")
	}
	var tags []string
	if b.PainTags != nil {
		for _, t := range *b.PainTags {
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "" || slices.Contains(tags, t) {
				continue
			}
			if utf8.RuneCountInString(t) > maxPainTagLen {
				return invalid(fmt.Sprintf("pain tags are limited to %d characters", maxPainTagLen))
			}
			tags = append(tags, t)
		}
	}
	if len(tags) > maxPainTags {
		return invalid(fmt.Sprintf("at most %d pain tags", maxPainTags))
	}
	var notes *string
	if b.Notes != nil && strings.TrimSpace(*b.Notes) != "" {
		notes = b.Notes
	}
	var feel *int16
	if b.Feel != nil {
		feel = new(int16(*b.Feel)) //nolint:gosec // G115: checked to be 0–100
	}

	err := s.ingest.Annotate(ctx, sessionFrom(ctx).userID, req.Id, ingest.Annotation{
		RPE: b.Rpe, Feel: feel, Notes: notes, PainTags: tags,
		WeatherTemperature: b.WeatherTemperatureC, WeatherCondition: (*string)(b.WeatherCondition), GearID: b.GearId,
	})
	if errors.Is(err, ingest.ErrNotFound) {
		return nil, errActivityNotFound
	}
	if errors.Is(err, ingest.ErrGearNotFound) {
		return invalid("no such gear")
	}
	if err != nil {
		return nil, fmt.Errorf("annotating: %w", err)
	}
	a, err := s.activity(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out, err := s.detail(ctx, a)
	if err != nil {
		return nil, err
	}
	return SetActivityAnnotation200JSONResponse(out), nil
}

func recordedWorkout(w db.RecordedWorkout) (*RecordedWorkout, error) {
	var steps []importer.WorkoutStep
	if err := json.Unmarshal(w.Steps, &steps); err != nil {
		return nil, fmt.Errorf("decoding workout steps: %w", err)
	}
	out := &RecordedWorkout{Name: w.Name, Steps: make([]WorkoutStep, len(steps))}
	for i, st := range steps {
		out.Steps[i] = WorkoutStep{
			Index: st.Index, Name: text(st.Name), Intensity: text(st.Intensity), DurationType: st.DurationType,
			Duration: st.Duration, TargetType: text(st.TargetType), TargetLow: st.TargetLow, TargetHigh: st.TargetHigh,
			TargetZone: st.TargetZone, RepeatFrom: st.RepeatFrom, RepeatCount: st.RepeatCount,
			ExerciseCategory: st.ExerciseCategory, ExerciseNumber: st.ExerciseNumber, WeightKg: st.Weight,
		}
	}
	return out, nil
}

// indexCursor is the position after a page's last lap, length or set.
type indexCursor struct {
	Index int32 `json:"i"`
}

// childPage validates the paging parameters of a lap, length or set list and
// returns the index to continue after and the page size.
func (s *Server) childPage(cursor *Cursor, limit *Limit) (after int32, size int, err error) {
	size = defaultPageSize
	if limit != nil {
		size = *limit
	}
	if size < 1 || size > maxPageSize {
		return 0, 0, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("limit must be 1 to %d", maxPageSize)}
	}
	after = -1
	if cursor != nil {
		var c indexCursor
		if err := s.cursors.Decode(*cursor, &c); err != nil {
			return 0, 0, err
		}
		after = c.Index
	}
	return after, size, nil
}

// trimPage cuts the extra row fetched past size and returns the next cursor.
func trimPage[T any](s *Server, rows []T, size int, index func(T) int32) ([]T, *string, error) {
	if len(rows) <= size {
		return rows, nil, nil
	}
	rows = rows[:size]
	next, err := s.cursors.Encode(indexCursor{Index: index(rows[size-1])})
	if err != nil {
		return nil, nil, err
	}
	return rows, &next, nil
}

// ListLaps pages through an activity's laps.
func (s *Server) ListLaps(ctx context.Context, req ListLapsRequestObject) (ListLapsResponseObject, error) {
	if _, err := s.activity(ctx, req.Id); err != nil {
		return nil, err
	}
	after, size, err := s.childPage(req.Params.Cursor, req.Params.Limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListLaps(ctx, db.ListLapsParams{UserID: sessionFrom(ctx).userID, ActivityID: req.Id, After: after, MaxRows: int32(size + 1)}) //nolint:gosec // G115: size ≤ 100
	if err != nil {
		return nil, fmt.Errorf("listing laps: %w", err)
	}
	rows, next, err := trimPage(s, rows, size, func(l db.Lap) int32 { return l.LapIndex })
	if err != nil {
		return nil, err
	}
	page := LapPage{Items: make([]Lap, len(rows)), NextCursor: next}
	for i, l := range rows {
		page.Items[i] = Lap{
			Index: int(l.LapIndex), StartTime: l.StartTime, ElapsedTimeS: l.ElapsedTimeS, TimerTimeS: l.TimerTimeS,
			DistanceM: l.DistanceM, AscentM: l.AscentM, DescentM: l.DescentM, AvgSpeedMS: l.AvgSpeedMS, MaxSpeedMS: l.MaxSpeedMS,
			AvgHrBpm: intPtr(l.AvgHrBpm), MaxHrBpm: intPtr(l.MaxHrBpm), AvgPowerW: intPtr(l.AvgPowerW), MaxPowerW: intPtr(l.MaxPowerW),
			AvgCadence: l.AvgCadence, Intensity: (*LapIntensity)(l.Intensity), WorkoutStepIndex: int32Ptr(l.WorkoutStepIndex),
		}
	}
	return ListLaps200JSONResponse(page), nil
}

// ListSwimLengths pages through an activity's pool lengths, with SWOLF computed.
func (s *Server) ListSwimLengths(ctx context.Context, req ListSwimLengthsRequestObject) (ListSwimLengthsResponseObject, error) {
	if _, err := s.activity(ctx, req.Id); err != nil {
		return nil, err
	}
	after, size, err := s.childPage(req.Params.Cursor, req.Params.Limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListSwimLengths(ctx, db.ListSwimLengthsParams{UserID: sessionFrom(ctx).userID, ActivityID: req.Id, After: after, MaxRows: int32(size + 1)}) //nolint:gosec // G115: size ≤ 100
	if err != nil {
		return nil, fmt.Errorf("listing lengths: %w", err)
	}
	rows, next, err := trimPage(s, rows, size, func(l db.SwimLength) int32 { return l.LengthIndex })
	if err != nil {
		return nil, err
	}
	page := SwimLengthPage{Items: make([]SwimLength, len(rows)), NextCursor: next}
	for i, l := range rows {
		page.Items[i] = SwimLength{
			Index: int(l.LengthIndex), StartTime: l.StartTime, ElapsedTimeS: l.ElapsedTimeS, TimerTimeS: l.TimerTimeS,
			Active: l.Active, Stroke: (*SwimLengthStroke)(l.Stroke), Strokes: intPtr(l.Strokes), AvgSpeedMS: l.AvgSpeedMS,
			Swolf: importer.Length{Active: l.Active, TimerTime: l.TimerTimeS, Strokes: i16f(l.Strokes)}.SWOLF(),
		}
	}
	return ListSwimLengths200JSONResponse(page), nil
}

// ListStrengthSets pages through an activity's strength sets.
func (s *Server) ListStrengthSets(ctx context.Context, req ListStrengthSetsRequestObject) (ListStrengthSetsResponseObject, error) {
	if _, err := s.activity(ctx, req.Id); err != nil {
		return nil, err
	}
	after, size, err := s.childPage(req.Params.Cursor, req.Params.Limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListStrengthSets(ctx, db.ListStrengthSetsParams{UserID: sessionFrom(ctx).userID, ActivityID: req.Id, After: after, MaxRows: int32(size + 1)}) //nolint:gosec // G115: size ≤ 100
	if err != nil {
		return nil, fmt.Errorf("listing sets: %w", err)
	}
	rows, next, err := trimPage(s, rows, size, func(st db.StrengthSet) int32 { return st.SetIndex })
	if err != nil {
		return nil, err
	}
	page := StrengthSetPage{Items: make([]StrengthSet, len(rows)), NextCursor: next}
	for i, st := range rows {
		page.Items[i] = StrengthSet{
			Index: int(st.SetIndex), StartTime: st.StartTime, DurationS: st.DurationS, Active: st.Active, Reps: intPtr(st.Reps),
			WeightKg: st.WeightKg, ExerciseName: st.ExerciseName, ExerciseCategory: int32Ptr(st.ExerciseCategory),
			ExerciseNumber: int32Ptr(st.ExerciseNumber), WorkoutStepIndex: int32Ptr(st.WorkoutStepIndex),
		}
	}
	return ListStrengthSets200JSONResponse(page), nil
}

// streamCursor is the time of a page's last sample.
type streamCursor struct {
	After time.Time `json:"t"`
}

// channels are the stream columns: how to read each from a sample and where it goes.
var channels = []struct {
	set  func(*ActivityStreams, *[]*float64)
	read func(db.ActivityStream) *float64
}{
	{func(o *ActivityStreams, v *[]*float64) { o.Lat = v }, func(r db.ActivityStream) *float64 { return r.LatDeg }},
	{func(o *ActivityStreams, v *[]*float64) { o.Lon = v }, func(r db.ActivityStream) *float64 { return r.LonDeg }},
	{func(o *ActivityStreams, v *[]*float64) { o.AltitudeM = v }, func(r db.ActivityStream) *float64 { return f64(r.AltitudeM) }},
	{func(o *ActivityStreams, v *[]*float64) { o.HrBpm = v }, func(r db.ActivityStream) *float64 { return i16f(r.HrBpm) }},
	{func(o *ActivityStreams, v *[]*float64) { o.PowerW = v }, func(r db.ActivityStream) *float64 { return i16f(r.PowerW) }},
	{func(o *ActivityStreams, v *[]*float64) { o.Cadence = v }, func(r db.ActivityStream) *float64 { return f64(r.Cadence) }},
	{func(o *ActivityStreams, v *[]*float64) { o.SpeedMS = v }, func(r db.ActivityStream) *float64 { return f64(r.SpeedMS) }},
	{func(o *ActivityStreams, v *[]*float64) { o.DistanceM = v }, func(r db.ActivityStream) *float64 { return r.DistanceM }},
	{func(o *ActivityStreams, v *[]*float64) { o.TemperatureC = v }, func(r db.ActivityStream) *float64 { return f64(r.TemperatureC) }},
}

// GetActivityStreams returns samples as columns: full resolution in pages, or
// reduced per channel with LTTB when maxPoints is set.
func (s *Server) GetActivityStreams(ctx context.Context, req GetActivityStreamsRequestObject) (GetActivityStreamsResponseObject, error) {
	a, err := s.activity(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	p := req.Params
	if p.MaxPoints != nil && (*p.MaxPoints < 3 || *p.MaxPoints > 10000) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: "maxPoints must be 3 to 10000"}
	}
	if p.MaxPoints != nil && p.Cursor != nil {
		return nil, &Error{Kind: KindInvalidRequest, Detail: "a reduced stream has no pages"}
	}
	params := db.ListActivityStreamsParams{UserID: sessionFrom(ctx).userID, ActivityID: a.ID, MaxRows: streamPageSize + 1}
	if p.MaxPoints != nil {
		params.MaxRows = maxDecimatedSamples
	}
	if p.Cursor != nil {
		var c streamCursor
		if err := s.cursors.Decode(*p.Cursor, &c); err != nil {
			return nil, err
		}
		params.After = c.After
	}
	rows, err := s.q.ListActivityStreams(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("listing streams: %w", err)
	}

	var next *string
	if p.MaxPoints == nil && len(rows) > streamPageSize {
		rows = rows[:streamPageSize]
		c, err := s.cursors.Encode(streamCursor{After: rows[streamPageSize-1].Time})
		if err != nil {
			return nil, err
		}
		next = &c
	}
	if p.MaxPoints != nil {
		rows = decimate(rows, a.StartTime, *p.MaxPoints)
	}
	out := columns(rows, a.StartTime)
	out.NextCursor = next
	return GetActivityStreams200JSONResponse(out), nil
}

// decimate keeps, for each channel, the samples LTTB picks from its recorded
// values; the result is their union in time order.
func decimate(rows []db.ActivityStream, start time.Time, maxPoints int) []db.ActivityStream {
	keep := make([]bool, len(rows))
	found := false
	for _, ch := range channels {
		var idx []int
		var xs, ys []float64
		for i, r := range rows {
			if v := ch.read(r); v != nil {
				idx = append(idx, i)
				xs = append(xs, r.Time.Sub(start).Seconds())
				ys = append(ys, *v)
			}
		}
		for _, k := range analytics.LTTB(xs, ys, maxPoints) {
			keep[idx[k]], found = true, true
		}
	}
	if !found { // no channel recorded anything: keep evenly spread times
		xs := make([]float64, len(rows))
		for i, r := range rows {
			xs[i] = r.Time.Sub(start).Seconds()
		}
		for _, k := range analytics.LTTB(xs, xs, maxPoints) {
			keep[k] = true
		}
	}
	out := make([]db.ActivityStream, 0, maxPoints)
	for i, r := range rows {
		if keep[i] {
			out = append(out, r)
		}
	}
	return out
}

// columns lays samples out as arrays; channels with no recorded value are omitted.
func columns(rows []db.ActivityStream, start time.Time) ActivityStreams {
	out := ActivityStreams{TimeS: make([]float64, len(rows))}
	for i, r := range rows {
		out.TimeS[i] = r.Time.Sub(start).Seconds()
	}
	for _, ch := range channels {
		col := make([]*float64, len(rows))
		recorded := false
		for i, r := range rows {
			col[i] = ch.read(r)
			recorded = recorded || col[i] != nil
		}
		if recorded {
			ch.set(&out, &col)
		}
	}
	return out
}

func f64(v *float32) *float64 {
	if v == nil {
		return nil
	}
	return new(float64(*v))
}

func i16f(v *int16) *float64 {
	if v == nil {
		return nil
	}
	return new(float64(*v))
}

func int32Ptr(v *int32) *int {
	if v == nil {
		return nil
	}
	return new(int(*v))
}

func text(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
