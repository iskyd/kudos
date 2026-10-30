package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/jobs"
	"github.com/iskyd/kudos/internal/store/db"
)

// Edit kinds. Every edit is a row in activity_edits; samples are never touched.
const (
	EditSport     = "sport"
	EditTrim      = "trim"
	EditExcludeHR = "exclude_hr"
	EditMerge     = "merge"
	EditSplit     = "split"
)

// MaxEdits bounds the edits one activity can hold.
const MaxEdits = 50

// ErrEditNotFound is returned when undoing an edit the activity does not have.
var ErrEditNotFound = errors.New("no such edit")

// InvalidEditError explains why an edit cannot be applied.
type InvalidEditError string

func (e InvalidEditError) Error() string { return string(e) }

// errNoSamples ends a recomputation whose view holds no sample.
var errNoSamples = errors.New("no samples")

// Edit is one edit as stored. Times are absolute; which fields are set depends on Kind.
type Edit struct {
	Kind    string     `json:"-"`
	Sport   string     `json:"sport,omitempty"`
	Subtype string     `json:"subtype,omitempty"`
	From    *time.Time `json:"from,omitempty"` // trim, exclude_hr; inclusive
	To      *time.Time `json:"to,omitempty"`
	At      *time.Time `json:"at,omitempty"` // split: the second part starts here
	// The activity merged in, or the second part of a split.
	ActivityID int64 `json:"activity_id,omitempty"`
}

// DecodeEdit reads a stored edit.
func DecodeEdit(e db.ActivityEdit) (Edit, error) {
	out := Edit{Kind: e.Kind}
	if err := json.Unmarshal(e.Params, &out); err != nil {
		return Edit{}, fmt.Errorf("decoding edit %d: %w", e.ID, err)
	}
	return out, nil
}

// Overrides are an activity's effective values, cached in activities.overrides
// whenever its edits change. The recorded columns stay as recorded.
type Overrides struct {
	Sport     *string    `json:"sport,omitempty"`
	Subtype   *string    `json:"subtype,omitempty"`
	StartTime *time.Time `json:"start_time,omitempty"`
	// Replaces every recorded summary value when set; a nil field is unknown.
	Summary *Summary `json:"summary,omitempty"`
}

// Summary holds effective summary values; keys match the activities columns.
type Summary struct {
	ElapsedTimeS    *float64 `json:"elapsed_time_s"`
	TimerTimeS      *float64 `json:"timer_time_s"`
	DistanceM       *float64 `json:"distance_m"`
	AscentM         *float32 `json:"ascent_m"`
	DescentM        *float32 `json:"descent_m"`
	AvgSpeedMS      *float32 `json:"avg_speed_m_s"`
	MaxSpeedMS      *float32 `json:"max_speed_m_s"`
	AvgHrBpm        *int16   `json:"avg_hr_bpm"`
	MaxHrBpm        *int16   `json:"max_hr_bpm"`
	AvgPowerW       *int16   `json:"avg_power_w"`
	MaxPowerW       *int16   `json:"max_power_w"`
	AvgCadence      *float32 `json:"avg_cadence"`
	MaxCadence      *float32 `json:"max_cadence"`
	AvgTemperatureC *float32 `json:"avg_temperature_c"`
	CaloriesKcal    *int32   `json:"calories_kcal"`
}

// Effective returns the activity with its edits applied: the view every read uses.
func Effective(a db.Activity) (db.Activity, error) {
	var o Overrides
	if err := json.Unmarshal(a.Overrides, &o); err != nil {
		return db.Activity{}, fmt.Errorf("decoding overrides of activity %d: %w", a.ID, err)
	}
	if o.Sport != nil && o.Subtype != nil {
		a.Sport, a.Subtype = *o.Sport, *o.Subtype
	}
	if o.StartTime != nil {
		a.StartTime = *o.StartTime
	}
	if s := o.Summary; s != nil {
		a.ElapsedTimeS, a.TimerTimeS, a.DistanceM = s.ElapsedTimeS, s.TimerTimeS, s.DistanceM
		a.AscentM, a.DescentM, a.AvgSpeedMS, a.MaxSpeedMS = s.AscentM, s.DescentM, s.AvgSpeedMS, s.MaxSpeedMS
		a.AvgHrBpm, a.MaxHrBpm, a.AvgPowerW, a.MaxPowerW = s.AvgHrBpm, s.MaxHrBpm, s.AvgPowerW, s.MaxPowerW
		a.AvgCadence, a.MaxCadence, a.AvgTemperatureC, a.CaloriesKcal = s.AvgCadence, s.MaxCadence, s.AvgTemperatureC, s.CaloriesKcal
	}
	return a, nil
}

// Bounds standing in for an open window.
var (
	minTime = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	maxTime = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
)

// Segment is a stretch of one activity's stored samples, [From, To).
type Segment struct {
	ActivityID int64
	From, To   time.Time
}

// Range is a closed time range.
type Range struct{ From, To time.Time }

// View says which stored samples make up an activity once its edits apply:
// segments in time order, with heart rate dropped inside ExcludedHR.
type View struct {
	Segments   []Segment
	ExcludedHR []Range
}

// view builds the activity's view from its edits, oldest first.
func view(ctx context.Context, q *db.Queries, a db.Activity, edits []Edit) (View, error) {
	var v View
	if a.SplitOf != nil {
		// The second part of a split reads its parent's samples from its own start.
		v.Segments = []Segment{{ActivityID: *a.SplitOf, From: a.StartTime, To: maxTime}}
	} else {
		v.Segments = []Segment{{ActivityID: a.ID, From: minTime, To: maxTime}}
	}
	var trim *Edit
	for i, e := range edits {
		switch e.Kind {
		case EditMerge:
			b, err := q.GetActivity(ctx, db.GetActivityParams{UserID: a.UserID, ID: e.ActivityID})
			if err != nil {
				return View{}, fmt.Errorf("reading merged activity: %w", err)
			}
			v.Segments = append(v.Segments, Segment{ActivityID: b.ID, From: minTime, To: maxTime})
			if b.StartTime.Before(a.StartTime) {
				slices.Reverse(v.Segments)
			}
		case EditSplit:
			v.Segments[0].To = *e.At
		case EditTrim:
			trim = &edits[i] // the latest trim wins
		case EditExcludeHR:
			v.ExcludedHR = append(v.ExcludedHR, Range{From: *e.From, To: *e.To})
		}
	}
	if trim != nil {
		for i := range v.Segments {
			s := &v.Segments[i]
			s.From = later(s.From, *trim.From)
			// Trim bounds are inclusive and a segment end is not; Postgres keeps microseconds.
			s.To = earlier(s.To, trim.To.Add(time.Microsecond))
		}
	}
	return v, nil
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// segmentSamples reads up to limit samples after after, per segment, with
// excluded heart rate dropped.
func segmentSamples(ctx context.Context, q *db.Queries, userID int64, v View, after time.Time, limit int) ([][]db.ActivityStream, error) {
	out := make([][]db.ActivityStream, len(v.Segments))
	n := 0
	for i, seg := range v.Segments {
		if n >= limit || !seg.To.After(after) {
			continue
		}
		rows, err := q.ListActivityStreamsWindow(ctx, db.ListActivityStreamsWindowParams{
			UserID: userID, ActivityID: seg.ActivityID, After: after, FromTime: seg.From, ToTime: seg.To,
			MaxRows: int32(limit - n), //nolint:gosec // G115: limit is a page size
		})
		if err != nil {
			return nil, fmt.Errorf("reading samples: %w", err)
		}
		for j := range rows {
			for _, r := range v.ExcludedHR {
				if t := rows[j].Time; !t.Before(r.From) && !t.After(r.To) {
					rows[j].HrBpm = nil
				}
			}
		}
		out[i], n = rows, n+len(rows)
	}
	return out, nil
}

// View returns the activity's view after its edits.
func (s *Service) View(ctx context.Context, a db.Activity) (View, error) {
	q := db.New(s.pool)
	edits, err := listEdits(ctx, q, a.UserID, a.ID)
	if err != nil {
		return View{}, err
	}
	return view(ctx, q, a, edits)
}

// Samples returns up to limit samples of the view after after (nil for the
// start), in time order.
func (s *Service) Samples(ctx context.Context, userID int64, v View, after *time.Time, limit int) ([]db.ActivityStream, error) {
	from := minTime
	if after != nil {
		from = *after
	}
	segs, err := segmentSamples(ctx, db.New(s.pool), userID, v, from, limit)
	if err != nil {
		return nil, err
	}
	return slices.Concat(segs...), nil
}

func listEdits(ctx context.Context, q *db.Queries, userID, activityID int64) ([]Edit, error) {
	rows, err := q.ListActivityEdits(ctx, db.ListActivityEditsParams{UserID: userID, ActivityID: activityID})
	if err != nil {
		return nil, fmt.Errorf("listing edits: %w", err)
	}
	out := make([]Edit, len(rows))
	for i, r := range rows {
		if out[i], err = DecodeEdit(r); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// maxSummarySamples bounds what a recomputation reads: two 55-hour activities at 1 s.
// shortcut: longer views are summarized from their first 400 000 samples; page the read when one exists.
const maxSummarySamples = 400_000

// recompute stores the activity's effective values after its edits changed and
// queues its derived values (invariant 23).
func (s *Service) recompute(ctx context.Context, tx pgx.Tx, userID, id int64) error {
	q := db.New(tx)
	a, err := q.GetActivity(ctx, db.GetActivityParams{UserID: userID, ID: id})
	if err != nil {
		return fmt.Errorf("reading activity: %w", err)
	}
	edits, err := listEdits(ctx, q, userID, id)
	if err != nil {
		return err
	}
	var o Overrides
	windowed, hrExcluded := a.SplitOf != nil, false
	for _, e := range edits {
		switch e.Kind {
		case EditSport:
			o.Sport, o.Subtype = &e.Sport, &e.Subtype
		case EditExcludeHR:
			hrExcluded = true
		default:
			windowed = true
		}
	}
	if windowed || hrExcluded {
		v, err := view(ctx, q, a, edits)
		if err != nil {
			return err
		}
		segs, err := segmentSamples(ctx, q, userID, v, minTime, maxSummarySamples)
		if err != nil {
			return err
		}
		sum, ok := analytics.Summarize(samples(segs))
		if !ok {
			return errNoSamples
		}
		if windowed {
			// Elevation and energy cannot be recomputed from the samples: unknown.
			o.StartTime = &sum.Start
			o.Summary = &Summary{
				ElapsedTimeS: &sum.ElapsedS, TimerTimeS: &sum.TimerS, DistanceM: sum.DistanceM,
				AvgSpeedMS: f32(sum.AvgSpeed), MaxSpeedMS: f32(sum.MaxSpeed), AvgHrBpm: i16(sum.AvgHR), MaxHrBpm: i16(sum.MaxHR),
				AvgPowerW: i16(sum.AvgPower), MaxPowerW: i16(sum.MaxPower), AvgCadence: f32(sum.AvgCadence),
				MaxCadence: f32(sum.MaxCadence), AvgTemperatureC: f32(sum.AvgTemperature),
			}
		} else {
			// Only heart rate changes; the rest stays as recorded.
			o.Summary = &Summary{
				ElapsedTimeS: a.ElapsedTimeS, TimerTimeS: a.TimerTimeS, DistanceM: a.DistanceM, AscentM: a.AscentM, DescentM: a.DescentM,
				AvgSpeedMS: a.AvgSpeedMS, MaxSpeedMS: a.MaxSpeedMS, AvgHrBpm: i16(sum.AvgHR), MaxHrBpm: i16(sum.MaxHR),
				AvgPowerW: a.AvgPowerW, MaxPowerW: a.MaxPowerW, AvgCadence: a.AvgCadence, MaxCadence: a.MaxCadence,
				AvgTemperatureC: a.AvgTemperatureC, CaloriesKcal: a.CaloriesKcal,
			}
		}
	}
	b, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("encoding overrides: %w", err)
	}
	if err := q.SetActivityOverrides(ctx, db.SetActivityOverridesParams{UserID: userID, ID: id, Overrides: b}); err != nil {
		return fmt.Errorf("storing overrides: %w", err)
	}
	if _, err := s.jobs.InsertTx(ctx, tx, jobs.DerivedArgs{UserID: userID, ActivityID: id}, nil); err != nil {
		return fmt.Errorf("enqueueing derived values: %w", err)
	}
	return nil
}

func samples(segs [][]db.ActivityStream) [][]analytics.Sample {
	out := make([][]analytics.Sample, len(segs))
	for i, rows := range segs {
		out[i] = make([]analytics.Sample, len(rows))
		for j, r := range rows {
			out[i][j] = analytics.Sample{
				Time: r.Time, HR: f64(r.HrBpm), Power: f64(r.PowerW), Cadence: f64(r.Cadence),
				Speed: f64(r.SpeedMS), Distance: r.DistanceM, Temperature: f64(r.TemperatureC),
			}
		}
	}
	return out
}

func f64[T int16 | float32](v *T) *float64 {
	if v == nil {
		return nil
	}
	return new(float64(*v))
}

func joins(e Edit) bool { return e.Kind == EditMerge || e.Kind == EditSplit }

// AddEdit stores an edit on the user's activity and recomputes what it changes;
// a split creates the second part here.
func (s *Service) AddEdit(ctx context.Context, userID, activityID int64, e Edit) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		a, err := q.GetActivity(ctx, db.GetActivityParams{UserID: userID, ID: activityID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("reading activity: %w", err)
		}
		if a.MergedInto != nil {
			return InvalidEditError("this activity was merged into another; edit that one")
		}
		edits, err := listEdits(ctx, q, userID, activityID)
		if err != nil {
			return err
		}
		if len(edits) >= MaxEdits {
			return InvalidEditError(fmt.Sprintf("an activity holds at most %d edits", MaxEdits))
		}
		joined := a.SplitOf != nil || slices.ContainsFunc(edits, joins)
		noSamples := InvalidEditError("nothing would be left of the recorded samples")
		touched := []int64{activityID}

		switch e.Kind {
		case EditSport:
		case EditTrim, EditExcludeHR:
			if e.From == nil || e.To == nil || !e.From.Before(*e.To) {
				return InvalidEditError("the range must end after it starts")
			}
		case EditSplit:
			if joined {
				return InvalidEditError("an activity that was merged or split cannot be split again")
			}
			eff, err := Effective(a)
			if err != nil {
				return err
			}
			second, err := q.InsertSplitActivity(ctx, db.InsertSplitActivityParams{
				UserID: userID, SplitOf: &a.ID, Sport: eff.Sport, Subtype: eff.Subtype, StartTime: *e.At, UtcOffsetS: a.UtcOffsetS,
			})
			if err != nil {
				return fmt.Errorf("creating the second part: %w", err)
			}
			e.ActivityID = second
			touched = append(touched, second)
			noSamples = "the split must leave recorded samples on both sides"
		case EditMerge:
			if err := checkMerge(ctx, q, a, joined, e.ActivityID); err != nil {
				return err
			}
			if err := q.SetActivityMergedInto(ctx, db.SetActivityMergedIntoParams{UserID: userID, ID: e.ActivityID, MergedInto: &a.ID}); err != nil {
				return fmt.Errorf("merging: %w", err)
			}
			touched = append(touched, e.ActivityID)
		default:
			return InvalidEditError("unknown edit")
		}

		params, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("encoding edit: %w", err)
		}
		if _, err := q.InsertActivityEdit(ctx, db.InsertActivityEditParams{UserID: userID, ActivityID: activityID, Kind: e.Kind, Params: params}); err != nil {
			return fmt.Errorf("storing edit: %w", err)
		}
		for _, id := range touched {
			if err := s.recompute(ctx, tx, userID, id); errors.Is(err, errNoSamples) {
				return noSamples
			} else if err != nil {
				return err
			}
		}
		if e.Kind == EditMerge {
			return checkSegments(ctx, q, userID, activityID)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("editing activity: %w", err)
	}
	return nil
}

// checkMerge refuses merges that would be ambiguous: chains, splits and overlaps.
func checkMerge(ctx context.Context, q *db.Queries, a db.Activity, joined bool, otherID int64) error {
	if otherID == a.ID {
		return InvalidEditError("an activity cannot be merged with itself")
	}
	if joined {
		return InvalidEditError("an activity that was merged or split cannot be merged again")
	}
	b, err := q.GetActivity(ctx, db.GetActivityParams{UserID: a.UserID, ID: otherID})
	if errors.Is(err, pgx.ErrNoRows) {
		return InvalidEditError("no such activity to merge")
	}
	if err != nil {
		return fmt.Errorf("reading the activity to merge: %w", err)
	}
	bEdits, err := listEdits(ctx, q, a.UserID, b.ID)
	if err != nil {
		return err
	}
	if b.MergedInto != nil || b.SplitOf != nil || slices.ContainsFunc(bEdits, joins) {
		return InvalidEditError("an activity that was merged or split cannot be merged again")
	}
	if overlap(a, b) {
		return InvalidEditError("the activities overlap in time")
	}
	return nil
}

func overlap(a, b db.Activity) bool {
	end := func(x db.Activity) time.Time {
		if x.ElapsedTimeS == nil {
			return x.StartTime
		}
		return x.StartTime.Add(time.Duration(*x.ElapsedTimeS * float64(time.Second)))
	}
	return a.StartTime.Before(end(b)) && b.StartTime.Before(end(a))
}

// checkSegments makes sure both activities of a merge bring samples.
func checkSegments(ctx context.Context, q *db.Queries, userID, id int64) error {
	{
		a, err := q.GetActivity(ctx, db.GetActivityParams{UserID: userID, ID: id})
		if err != nil {
			return fmt.Errorf("reading activity: %w", err)
		}
		edits, err := listEdits(ctx, q, userID, id)
		if err != nil {
			return err
		}
		v, err := view(ctx, q, a, edits)
		if err != nil {
			return err
		}
		for _, seg := range v.Segments {
			rows, err := segmentSamples(ctx, q, userID, View{Segments: []Segment{seg}}, minTime, 1)
			if err != nil {
				return err
			}
			if len(rows[0]) == 0 {
				return InvalidEditError("both activities need recorded samples")
			}
		}
	}
	return nil
}

// UndoEdit removes one edit and recomputes the activity. Undoing a merge brings
// the merged activity back; undoing a split deletes the second part, annotation included.
func (s *Service) UndoEdit(ctx context.Context, userID, activityID, editID int64) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		row, err := q.DeleteActivityEdit(ctx, db.DeleteActivityEditParams{UserID: userID, ActivityID: activityID, ID: editID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEditNotFound
		}
		if err != nil {
			return fmt.Errorf("deleting edit: %w", err)
		}
		e, err := DecodeEdit(row)
		if err != nil {
			return err
		}
		switch e.Kind {
		case EditMerge:
			if err := q.SetActivityMergedInto(ctx, db.SetActivityMergedIntoParams{UserID: userID, ID: e.ActivityID}); err != nil {
				return fmt.Errorf("unmerging: %w", err)
			}
			if err := s.recompute(ctx, tx, userID, e.ActivityID); err != nil {
				return err
			}
		case EditSplit:
			if _, err := q.DeleteSplitActivity(ctx, db.DeleteSplitActivityParams{UserID: userID, SplitOf: &activityID}); err != nil {
				return fmt.Errorf("deleting the second part: %w", err)
			}
		}
		// Undoing widens the view or leaves it as it was, so samples remain.
		return s.recompute(ctx, tx, userID, activityID)
	})
	if err != nil {
		return fmt.Errorf("undoing edit: %w", err)
	}
	return nil
}
