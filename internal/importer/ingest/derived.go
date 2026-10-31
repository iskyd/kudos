package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/importer"
	"github.com/iskyd/kudos/internal/jobs"
	"github.com/iskyd/kudos/internal/store/db"
)

// derivedVersion versions the derived values' computation: raising it makes the
// recompute jobs redo every activity's.
const derivedVersion = 5

// recomputeBatch is how many activities or users a recompute job reads at once.
const recomputeBatch = 100

// ResolvedZones are the zones an activity uses: the versions valid on its local date.
type ResolvedZones struct {
	Activity                    db.Activity // after its edits
	Day                         time.Time
	Sport                       analytics.ZoneSport
	ZoneSetID, GeneralZoneSetID *int64
	Own, General                analytics.Thresholds
	Zones                       analytics.Zones
}

// ResolveZones finds the zone versions valid on the activity's local date, after
// its edits: its sport's and the general one (heart rate reserve).
func (s *Service) ResolveZones(ctx context.Context, a db.Activity) (ResolvedZones, error) {
	eff, err := Effective(a)
	if err != nil {
		return ResolvedZones{}, err
	}
	local := eff.StartTime.Add(time.Duration(eff.UtcOffsetS) * time.Second)
	out := ResolvedZones{
		Activity: eff,
		Day:      time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC),
		Sport:    analytics.ZoneSportOf(eff.Sport),
	}
	rows, err := db.New(s.pool).ListZoneSetsAt(ctx, db.ListZoneSetsAtParams{UserID: a.UserID, Day: out.Day})
	if err != nil {
		return ResolvedZones{}, fmt.Errorf("reading zone versions: %w", err)
	}
	for _, r := range rows {
		switch {
		case analytics.ZoneSport(r.Sport) == out.Sport:
			out.Own, out.ZoneSetID = ZoneThresholds(r), &r.ID
		case r.Sport == string(analytics.ZoneGeneral):
			out.General, out.GeneralZoneSetID = ZoneThresholds(r), &r.ID
		}
	}
	out.Zones = analytics.ZonesFor(out.Sport, out.Own, out.General)
	return out, nil
}

// ZoneThresholds reads a zone version's thresholds in SI units.
func ZoneThresholds(z db.ZoneSet) analytics.Thresholds {
	return analytics.Thresholds{
		LTHR: f64(z.LthrBpm), ThresholdSpeed: f64(z.ThresholdSpeedMS), FTP: f64(z.FtpW),
		CSS: f64(z.CssMS), MaxHR: f64(z.MaxHrBpm), RestingHR: f64(z.RestingHrBpm),
	}
}

// ComputeDerived computes and stores an activity's derived values (running and
// swim analytics included), best efforts and curve points from its edited
// samples and lengths and the zones valid on its date, and
// suggests the thresholds its curve implies. Recomputing gives the same values;
// an activity that no longer exists is skipped.
func (s *Service) ComputeDerived(ctx context.Context, userID, activityID int64) error {
	q := db.New(s.pool)
	a, err := q.GetActivity(ctx, db.GetActivityParams{UserID: userID, ID: activityID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading activity: %w", err)
	}
	v, err := s.View(ctx, a)
	if err != nil {
		return err
	}
	segs, err := s.Segments(ctx, userID, v, maxSummarySamples)
	if err != nil {
		return err
	}
	z, err := s.ResolveZones(ctx, a)
	if err != nil {
		return err
	}
	p := db.UpsertActivityDerivedParams{
		UserID: userID, ActivityID: activityID, Version: derivedVersion,
		ZoneSetID: z.ZoneSetID, GeneralZoneSetID: z.GeneralZoneSetID,
		HrZoneModel: text(z.Zones.HRModel), PaceZoneModel: text(z.Zones.PaceModel), PowerZoneModel: text(z.Zones.PowerModel),
		HrZoneS:    analytics.TimeInZones(segs, z.Zones.HR, func(s analytics.Sample) *float64 { return s.HR }),
		PaceZoneS:  analytics.TimeInZones(segs, z.Zones.Pace, func(s analytics.Sample) *float64 { return s.Speed }),
		PowerZoneS: analytics.TimeInZones(segs, z.Zones.Power, func(s analytics.Sample) *float64 { return s.Power }),
	}
	sum, ok := analytics.Summarize(segs)
	if ok {
		p.AvgHrBpm, p.AvgPowerW, p.AvgSpeedMS, p.AvgCadence = sum.AvgHR, sum.AvgPower, sum.AvgSpeed, sum.AvgCadence
	}
	if load, method, ok := analytics.Load(loadInput(z.Activity, z, segs, sum)); ok {
		p.Load, p.LoadMethod = &load, text(string(method))
	}
	dist := z.Activity.DistanceM
	if dist == nil {
		dist = sum.DistanceM
	}
	virtual := z.Activity.Subtype == "virtual"
	in := analytics.EffortInput{
		Sport: z.Activity.Sport, Virtual: virtual, DistanceM: dist, Segments: segs, PoolLengthM: f64(z.Activity.PoolLengthM),
	}
	switch z.Activity.Sport {
	case "running", "trail_running":
		// Running analytics read outdoor pace, like the pace curve.
		if !virtual {
			p.EfficiencyMPerBeat, p.DecouplingPct = analytics.EfficiencyFactor(sum.AvgSpeed, sum.AvgHR), analytics.Decoupling(segs)
			p.AvgVerticalOscillationM, p.AvgStanceTimeS, p.AvgStepLengthM = sum.AvgVerticalOscillation, sum.AvgStanceTime, sum.AvgStepLength
		}
	case "pool_swimming":
		if in.Lengths, err = s.lengths(ctx, userID, v); err != nil {
			return err
		}
		p.AvgSwolf = analytics.AvgSWOLF(in.Lengths)
	}
	efforts, curve := analytics.BestEfforts(in), analytics.Curves(in)
	ins := db.InsertBestEffortsParams{UserID: userID, ActivityID: activityID}
	for _, e := range efforts {
		ins.Efforts = append(ins.Efforts, string(e.Name))
		ins.EffortValues = append(ins.EffortValues, e.Value)
	}
	points := db.InsertCurvePointsParams{UserID: userID, ActivityID: activityID}
	for _, c := range curve {
		points.Kinds = append(points.Kinds, string(c.Kind))
		points.Durations = append(points.Durations, int32(c.DurationS)) //nolint:gosec // G115: durations are at most hours
		points.CurveValues = append(points.CurveValues, c.Value)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		if err := q.UpsertActivityDerived(ctx, p); err != nil {
			return fmt.Errorf("storing derived values: %w", err)
		}
		if err := q.DeleteBestEfforts(ctx, db.DeleteBestEffortsParams{UserID: userID, ActivityID: activityID}); err != nil {
			return fmt.Errorf("clearing best efforts: %w", err)
		}
		if err := q.InsertBestEfforts(ctx, ins); err != nil {
			return fmt.Errorf("storing best efforts: %w", err)
		}
		if err := q.DeleteCurvePoints(ctx, db.DeleteCurvePointsParams{UserID: userID, ActivityID: activityID}); err != nil {
			return fmt.Errorf("clearing curve points: %w", err)
		}
		if err := q.InsertCurvePoints(ctx, points); err != nil {
			return fmt.Errorf("storing curve points: %w", err)
		}
		return suggestEstimates(ctx, q, userID, activityID, z.Day, curve)
	})
	if err != nil {
		return fmt.Errorf("storing activity %d: %w", activityID, err)
	}
	return nil
}

// maxLengths bounds the pool lengths read per segment: 25 km in a 25 m pool.
// shortcut: longer swims keep their first 1000 lengths; page the read when one exists.
const maxLengths = 1000

// lengths returns the pool lengths within the view's segments, in order.
func (s *Service) lengths(ctx context.Context, userID int64, v View) ([]importer.Length, error) {
	q := db.New(s.pool)
	var out []importer.Length
	for _, seg := range v.Segments {
		rows, err := q.ListSwimLengths(ctx, db.ListSwimLengthsParams{UserID: userID, ActivityID: seg.ActivityID, After: -1, MaxRows: maxLengths})
		if err != nil {
			return nil, fmt.Errorf("reading lengths: %w", err)
		}
		for _, r := range rows {
			if r.StartTime.Before(seg.From) || !r.StartTime.Before(seg.To) {
				continue
			}
			out = append(out, importer.Length{
				StartTime: r.StartTime, ElapsedTime: r.ElapsedTimeS, TimerTime: r.TimerTimeS, Active: r.Active,
				Strokes: f64(r.Strokes),
			})
		}
	}
	return out, nil
}

// loadInput takes duration and speed from the activity's effective summary,
// which pool swims fill from lengths and manual activities from the user, and
// falls back to the samples' summary.
func loadInput(a db.Activity, z ResolvedZones, segs [][]analytics.Sample, sum analytics.Summary) analytics.LoadInput {
	in := analytics.LoadInput{
		Sport: z.Sport, DurationS: sum.TimerS, Segments: segs, AvgSpeed: sum.AvgSpeed, AvgHR: sum.AvgHR,
		Own: z.Own, General: z.General,
	}
	if a.TimerTimeS != nil && *a.TimerTimeS > 0 {
		in.DurationS = *a.TimerTimeS
		if a.DistanceM != nil {
			in.AvgSpeed = new(*a.DistanceM / *a.TimerTimeS)
		}
	}
	if a.Rpe != nil {
		in.RPE = new(float64(*a.Rpe))
	}
	return in
}

// RecomputeDerived computes the user's derived values that are missing, stale or
// from an older version, in batches, and returns how many it computed.
func (s *Service) RecomputeDerived(ctx context.Context, userID int64) (int, error) {
	q := db.New(s.pool)
	n := 0
	for after := int64(0); ; {
		ids, err := q.ListActivitiesNeedingDerived(ctx, db.ListActivitiesNeedingDerivedParams{
			UserID: userID, After: after, Version: derivedVersion, MaxRows: recomputeBatch,
		})
		if err != nil {
			return n, fmt.Errorf("listing activities to recompute: %w", err)
		}
		if len(ids) == 0 {
			return n, nil
		}
		for _, id := range ids {
			if err := s.ComputeDerived(ctx, userID, id); err != nil {
				return n, err
			}
			n++
		}
		after = ids[len(ids)-1]
	}
}

// ChangeZones runs a zone change with the invalidation it causes, in one
// transaction: change returns the first day it affects, the user's derived
// values from that day on are marked stale and a recompute is queued.
func (s *Service) ChangeZones(ctx context.Context, userID int64, change func(*db.Queries) (time.Time, error)) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		day, err := change(q)
		if err != nil {
			return err
		}
		if _, err := q.InvalidateDerivedFrom(ctx, db.InvalidateDerivedFromParams{UserID: userID, FromDay: day}); err != nil {
			return fmt.Errorf("invalidating derived values: %w", err)
		}
		if _, err := s.jobs.InsertTx(ctx, tx, jobs.RecomputeArgs{UserID: userID}, nil); err != nil {
			return fmt.Errorf("enqueueing recompute: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("changing zones: %w", err)
	}
	return nil
}

// recomputeAll queues a recompute for every user.
func (s *Service) recomputeAll(ctx context.Context) error {
	q := db.New(s.pool)
	for after := int64(0); ; {
		ids, err := q.ListUserIDs(ctx, db.ListUserIDsParams{After: after, MaxRows: recomputeBatch})
		if err != nil {
			return fmt.Errorf("listing users: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		params := make([]river.InsertManyParams, len(ids))
		for i, id := range ids {
			params[i] = river.InsertManyParams{Args: jobs.RecomputeArgs{UserID: id}}
		}
		if _, err := s.jobs.InsertMany(ctx, params); err != nil {
			return fmt.Errorf("enqueueing recomputes: %w", err)
		}
		after = ids[len(ids)-1]
	}
}

// Workers works the derived-values jobs. Service is set once the ingest service
// exists, which needs the job client these workers are registered with.
type Workers struct {
	Service *Service
}

// Register adds the derived-values workers.
func (w *Workers) Register(ws *river.Workers) {
	river.AddWorker(ws, &derivedWorker{w: w})
	river.AddWorker(ws, &recomputeWorker{w: w})
	river.AddWorker(ws, &recomputeAllWorker{w: w})
}

type derivedWorker struct {
	river.WorkerDefaults[jobs.DerivedArgs]
	w *Workers
}

func (d *derivedWorker) Work(ctx context.Context, j *river.Job[jobs.DerivedArgs]) error {
	return d.w.Service.ComputeDerived(ctx, j.Args.UserID, j.Args.ActivityID)
}

type recomputeWorker struct {
	river.WorkerDefaults[jobs.RecomputeArgs]
	w *Workers
}

func (r *recomputeWorker) Work(ctx context.Context, j *river.Job[jobs.RecomputeArgs]) error {
	_, err := r.w.Service.RecomputeDerived(ctx, j.Args.UserID)
	return err
}

type recomputeAllWorker struct {
	river.WorkerDefaults[jobs.RecomputeAllArgs]
	w *Workers
}

func (r *recomputeAllWorker) Work(ctx context.Context, _ *river.Job[jobs.RecomputeAllArgs]) error {
	return r.w.Service.recomputeAll(ctx)
}
