// Package seed generates years of synthetic but realistic training data for one
// account: about two activities a day across sports, recorded every second (the
// strength sessions with smart recording), plus daily health metrics. Activities
// are written as FIT files and imported like uploads, so their originals are
// kept and parse again. Positions loop around 0° N 0° E, far from anyone's home.
package seed

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"

	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/store/db"
)

// ErrNotEmpty is returned for an account that already has activities.
var ErrNotEmpty = errors.New("the account already has activities")

// Options choose what to generate.
type Options struct {
	Days int       // of history, ending the day before End
	End  time.Time // the first day not generated
	Seed uint64    // the same seed generates the same data
	// Progress, when set, is called after each generated day.
	Progress func(day time.Time, s Stats)
}

// Stats count what was generated.
type Stats struct {
	Days, Activities, Samples int
}

// Run generates o.Days of data for an account without activities: zone
// versions from the first day, daily metrics and activities. loc is the
// account's timezone, where the activities happen.
func Run(ctx context.Context, svc *ingest.Service, q *db.Queries, userID int64, loc *time.Location, o Options) (Stats, error) {
	var s Stats
	existing, err := q.ListActivities(ctx, db.ListActivitiesParams{
		UserID: userID, Sort: "date", BeforeKey: math.MaxFloat64, BeforeID: math.MaxInt64, MaxRows: 1,
	})
	if err != nil {
		return s, fmt.Errorf("checking the account: %w", err)
	}
	if len(existing) > 0 {
		return s, ErrNotEmpty
	}
	end := time.Date(o.End.Year(), o.End.Month(), o.End.Day(), 0, 0, 0, 0, time.UTC)
	first := end.AddDate(0, 0, -o.Days)
	if err := zoneVersions(ctx, q, userID, first); err != nil {
		return s, err
	}
	rng := rand.New(rand.NewPCG(o.Seed, 0x6b75646f73)) //nolint:gosec // G404: synthetic data, not security
	body := bodyState{restingHR: 50, hrv: 65, weight: 72}
	for day := first; day.Before(end); day = day.AddDate(0, 0, 1) {
		if err := q.UpsertDailyMetrics(ctx, body.next(rng, userID, day)); err != nil {
			return s, fmt.Errorf("storing daily metrics: %w", err)
		}
		for _, p := range dayPlan(rng, day, loc) {
			f, samples := p.generate(rng)
			var buf bytes.Buffer
			if err := encoder.New(&buf).Encode(&f); err != nil {
				return s, fmt.Errorf("encoding activity: %w", err)
			}
			if _, err := svc.Upload(ctx, userID, loc, "seed.fit", buf.Bytes()); err != nil {
				return s, fmt.Errorf("importing activity of %s: %w", day.Format(time.DateOnly), err)
			}
			s.Activities++
			s.Samples += samples
		}
		s.Days++
		if o.Progress != nil {
			o.Progress(day, s)
		}
	}
	return s, nil
}

// zoneVersions sets thresholds for every sport from the first day, so every
// activity gets zones and a training load.
func zoneVersions(ctx context.Context, q *db.Queries, userID int64, from time.Time) error {
	for _, z := range []db.CreateZoneSetParams{
		{Sport: "running", LthrBpm: ptr(int16(168)), ThresholdSpeedMS: ptr(float32(3.6))},
		{Sport: "cycling", LthrBpm: ptr(int16(162)), FtpW: ptr(int16(230))},
		{Sport: "swimming", CssMS: ptr(float32(1.15))},
		{Sport: "general", MaxHrBpm: ptr(int16(190)), RestingHrBpm: ptr(int16(50))},
	} {
		z.UserID, z.EffectiveFrom = userID, from
		if _, err := q.CreateZoneSet(ctx, z); err != nil {
			return fmt.Errorf("creating %s zones: %w", z.Sport, err)
		}
	}
	return nil
}

// bodyState is the slowly drifting state daily metrics come from.
type bodyState struct{ restingHR, hrv, weight float64 }

func (b *bodyState) next(rng *rand.Rand, userID int64, day time.Time) db.UpsertDailyMetricsParams {
	// Mean-reverting random walks around a fit adult's values.
	b.restingHR += 0.2*(50-b.restingHR) + rng.NormFloat64()
	b.hrv += 0.2*(65-b.hrv) + 3*rng.NormFloat64()
	b.weight += 0.05*(72-b.weight) + 0.15*rng.NormFloat64()
	sleep := 7.4*3600 + 2400*rng.NormFloat64()
	return db.UpsertDailyMetricsParams{
		UserID: userID, Day: day,
		RestingHrBpm: ptr(int16(math.Round(b.restingHR))), HrvMs: ptr(float32(math.Max(b.hrv, 15))),
		SleepS: ptr(int32(sleep)), SleepDeepS: ptr(int32(sleep * 0.18)), SleepLightS: ptr(int32(sleep * 0.55)),
		SleepRemS: ptr(int32(sleep * 0.22)), SleepAwakeS: ptr(int32(sleep * 0.05)),
		SleepScore:     ptr(int16(clamp(60+sleep/3600*3+5*rng.NormFloat64(), 0, 100))),
		Stress:         ptr(int16(clamp(30+8*rng.NormFloat64(), 0, 100))),
		BodyBatteryMax: ptr(int16(clamp(85+8*rng.NormFloat64(), 0, 100))),
		BodyBatteryMin: ptr(int16(clamp(25+8*rng.NormFloat64(), 0, 100))),
		Steps:          ptr(int32(clamp(11000+3000*rng.NormFloat64(), 0, 60000))),
		WeightKg:       ptr(float32(b.weight)),
	}
}

// kind is what an activity is.
type kind int

const (
	run kind = iota
	ride
	virtualRide
	swim
	strength
	hike
)

// planned is one activity of a day.
type planned struct {
	kind   kind
	start  time.Time // UTC
	offset time.Duration
}

// dayPlan picks a day's activities, about two on average: a morning run most
// days, a ride on more than half (two in five on Zwift), a lunchtime swim or
// strength session now and then and an occasional hike.
func dayPlan(rng *rand.Rand, day time.Time, loc *time.Location) []planned {
	var out []planned
	add := func(k kind, p float64, hour, minute int) {
		if rng.Float64() >= p {
			return
		}
		local := time.Date(day.Year(), day.Month(), day.Day(), hour, minute+rng.IntN(30), 0, 0, loc)
		_, off := local.Zone()
		out = append(out, planned{k, local.UTC(), time.Duration(off) * time.Second})
	}
	add(run, 0.7, 6, 30)
	add(swim, 0.3, 12, 0)
	add(strength, 0.3, 13, 30)
	add(hike, 0.1, 9, 30)
	if rng.Float64() < 0.4 {
		add(virtualRide, 0.55, 18, 0)
	} else {
		add(ride, 0.55, 18, 0)
	}
	return out
}

// generate returns the activity as a FIT file and its number of samples.
func (p planned) generate(rng *rand.Rand) (proto.FIT, int) {
	var a *filedef.Activity
	switch p.kind {
	case swim:
		a = p.poolSwim(rng)
	case strength:
		a = p.strengthSession(rng)
	default:
		a = p.endurance(rng)
	}
	end := a.Sessions[0].Timestamp
	a.FileId = *mesgdef.NewFileId(nil).SetType(typedef.FileActivity).SetManufacturer(typedef.ManufacturerDevelopment).
		SetProduct(1).SetSerialNumber(4242).SetTimeCreated(p.start)
	a.Activity = mesgdef.NewActivity(nil).SetTimestamp(end).SetLocalTimestamp(end.Add(p.offset)).SetNumSessions(1).
		SetType(typedef.ActivityManual).SetEvent(typedef.EventActivity).SetEventType(typedef.EventTypeStop).
		SetTotalTimerTimeScaled(a.Sessions[0].TotalTimerTimeScaled())
	return a.ToFIT(nil), len(a.Records)
}

// profile is how an endurance sport's samples vary.
type profile struct {
	sport          typedef.Sport
	sub            typedef.SubSport
	minutes        [2]int // shortest and longest
	speed, hr, cad float64
	power          float64 // W, 0 without a power meter
	gps, dynamics  bool
	lapM, climbM   float64 // lap distance and hill height
}

func (p planned) profile(rng *rand.Rand) profile {
	switch p.kind {
	case run:
		return profile{typedef.SportRunning, typedef.SubSportGeneric, [2]int{30, 100}, 2.9 + 0.6*rng.Float64(), 148, 86, 0, true, true, 1000, 15}
	case hike:
		return profile{typedef.SportHiking, typedef.SubSportGeneric, [2]int{120, 240}, 1.2, 112, 55, 0, true, false, 1000, 150}
	case virtualRide:
		return profile{typedef.SportCycling, typedef.SubSportVirtualActivity, [2]int{45, 90}, 8.5, 142, 90, 205, false, false, 10000, 0}
	default:
		power := 0.0
		if rng.Float64() < 0.6 {
			power = 185
		}
		return profile{typedef.SportCycling, typedef.SubSportRoad, [2]int{60, 180}, 7.5, 138, 86, power, true, false, 10000, 40}
	}
}

// endurance records a run, ride or hike every second, in laps of pr.lapM.
func (p planned) endurance(rng *rand.Rand) *filedef.Activity {
	pr := p.profile(rng)
	n := 60 * (pr.minutes[0] + rng.IntN(pr.minutes[1]-pr.minutes[0]+1))
	a := filedef.NewActivity()
	var tot, lap totals
	dist := 0.0
	const radius = 1500.0 // m, the loop around 0° N 0° E
	for i := range n {
		t := p.start.Add(time.Duration(i) * time.Second)
		x := float64(i)
		speed := pr.speed * (1 + 0.08*math.Sin(x/180) + 0.03*rng.NormFloat64())
		hr := pr.hr + 40*(speed/pr.speed-1) + 10*x/float64(n) + 2*rng.NormFloat64()
		dist += speed
		r := mesgdef.NewRecord(nil).SetTimestamp(t).SetHeartRate(uint8(clamp(hr, 60, 200))).
			SetCadence(uint8(clamp(pr.cad+2*rng.NormFloat64(), 0, 200))).
			SetDistanceScaled(dist).SetEnhancedSpeedScaled(speed).
			SetEnhancedAltitudeScaled(100 + pr.climbM*math.Sin(dist/2000))
		var watts float64
		if pr.power > 0 {
			watts = clamp(pr.power*(1+0.15*math.Sin(x/60)+0.1*rng.NormFloat64()), 0, 1500)
			r.SetPower(uint16(watts))
		}
		if pr.gps {
			theta := dist / radius
			r.SetPositionLat(semicircles(radius / 111_320 * math.Sin(theta))).
				SetPositionLong(semicircles(radius / 111_320 * math.Cos(theta)))
		}
		if pr.dynamics {
			r.SetVerticalOscillationScaled(clamp(90+4*rng.NormFloat64(), 50, 150)).
				SetStanceTimeScaled(clamp(240-20*(speed-3)+5*rng.NormFloat64(), 150, 350)).
				SetStepLengthScaled(speed / (2 * pr.cad / 60) * 1000)
		}
		a.Records = append(a.Records, r)
		tot.add(hr, watts, speed)
		lap.add(hr, watts, speed)
		if lap.dist >= pr.lapM || i == n-1 {
			end := t.Add(time.Second)
			a.Laps = append(a.Laps, mesgdef.NewLap(nil).SetTimestamp(end).SetStartTime(end.Add(-time.Duration(lap.n)*time.Second)).
				SetEvent(typedef.EventLap).SetEventType(typedef.EventTypeStop).SetSport(pr.sport).SetSubSport(pr.sub).
				SetTotalElapsedTimeScaled(float64(lap.n)).SetTotalTimerTimeScaled(float64(lap.n)).SetTotalDistanceScaled(lap.dist).
				SetEnhancedAvgSpeedScaled(lap.dist/float64(lap.n)).SetAvgHeartRate(lap.avgHR()).SetAvgPower(lap.avgPower()))
			lap = totals{}
		}
	}
	a.Sessions = []*mesgdef.Session{p.session(pr.sport, pr.sub, n, tot).SetTotalDistanceScaled(dist).
		SetEnhancedAvgSpeedScaled(dist / float64(n)).SetAvgPower(tot.avgPower()).SetAvgCadence(uint8(pr.cad))}
	return a
}

// poolSwim swims repeats of 200 m in a 25 m pool with 30 s rests; the watch
// records heart rate every 5 s and nothing else.
func (p planned) poolSwim(rng *rand.Rand) *filedef.Activity {
	a := filedef.NewActivity()
	t := p.start
	active := 0
	length := func(seconds float64, active bool) {
		l := mesgdef.NewLength(nil).SetStartTime(t).SetTimestamp(t.Add(dur(seconds))).
			SetEvent(typedef.EventLength).SetEventType(typedef.EventTypeStop).
			SetTotalElapsedTimeScaled(seconds).SetTotalTimerTimeScaled(seconds).SetLengthType(typedef.LengthTypeIdle)
		if active {
			l.SetLengthType(typedef.LengthTypeActive).SetSwimStroke(typedef.SwimStrokeFreestyle).
				SetTotalStrokes(uint16(14 + rng.IntN(5))).SetAvgSpeedScaled(25 / seconds) //nolint:gosec // G115: 14 to 18 strokes
		}
		a.Lengths = append(a.Lengths, l)
		t = t.Add(dur(seconds))
	}
	for range 6 + rng.IntN(7) {
		start := t
		for range 8 {
			length(21+5*rng.Float64(), true)
			active++
		}
		a.Laps = append(a.Laps, mesgdef.NewLap(nil).SetStartTime(start).SetTimestamp(t).
			SetEvent(typedef.EventLap).SetEventType(typedef.EventTypeStop).SetSport(typedef.SportSwimming).
			SetTotalElapsedTimeScaled(t.Sub(start).Seconds()).SetTotalTimerTimeScaled(t.Sub(start).Seconds()).SetTotalDistanceScaled(200))
		length(30, false)
	}
	n := int(t.Sub(p.start).Seconds())
	var tot totals
	for i := 0; i < n; i += 5 {
		hr := 130 + 15*math.Sin(float64(i)/120) + 3*rng.NormFloat64()
		a.Records = append(a.Records, mesgdef.NewRecord(nil).SetTimestamp(p.start.Add(time.Duration(i)*time.Second)).SetHeartRate(uint8(hr)))
		tot.add(hr, 0, 0)
	}
	a.Sessions = []*mesgdef.Session{p.session(typedef.SportSwimming, typedef.SubSportLapSwimming, n, tot).
		SetTotalDistanceScaled(float64(active) * 25).SetPoolLengthScaled(25).SetPoolLengthUnit(typedef.DisplayMeasureMetric).
		SetNumActiveLengths(uint16(active))}
	return a
}

// strengthSession lifts three sets of ten of five exercises with 90 s rests;
// the watch records heart rate every 1 to 6 s (smart recording).
func (p planned) strengthSession(rng *rand.Rand) *filedef.Activity {
	a := filedef.NewActivity()
	t := p.start
	for _, ex := range []typedef.ExerciseCategory{
		typedef.ExerciseCategorySquat, typedef.ExerciseCategoryBenchPress, typedef.ExerciseCategoryDeadlift,
		typedef.ExerciseCategoryRow, typedef.ExerciseCategoryShoulderPress,
	} {
		for range 3 {
			for _, active := range []bool{true, false} {
				s := mesgdef.NewSet(nil).SetStartTime(t).SetTimestamp(t).SetSetType(typedef.SetTypeRest).SetDurationScaled(90)
				seconds := 90.0
				if active {
					seconds = 35 + 10*rng.Float64()
					s.SetSetType(typedef.SetTypeActive).SetDurationScaled(seconds).SetRepetitions(10).
						SetWeightScaled(float64(20 + 5*rng.IntN(9))).SetCategory([]typedef.ExerciseCategory{ex}).SetCategorySubtype([]uint16{0})
				}
				a.UnrelatedMessages = append(a.UnrelatedMessages, s.ToMesg(nil))
				t = t.Add(dur(seconds))
			}
		}
	}
	n := int(t.Sub(p.start).Seconds())
	var tot totals
	for i := 0; i < n; i += 1 + rng.IntN(6) {
		hr := 100 + 25*math.Max(0, math.Sin(float64(i)/40)) + 3*rng.NormFloat64()
		a.Records = append(a.Records, mesgdef.NewRecord(nil).SetTimestamp(p.start.Add(time.Duration(i)*time.Second)).SetHeartRate(uint8(hr)))
		tot.add(hr, 0, 0)
	}
	a.Sessions = []*mesgdef.Session{p.session(typedef.SportTraining, typedef.SubSportStrengthTraining, n, tot)}
	return a
}

// session is the summary every activity has, over n seconds.
func (p planned) session(sport typedef.Sport, sub typedef.SubSport, n int, tot totals) *mesgdef.Session {
	return mesgdef.NewSession(nil).SetStartTime(p.start).SetTimestamp(p.start.Add(time.Duration(n) * time.Second)).
		SetEvent(typedef.EventSession).SetEventType(typedef.EventTypeStop).SetSport(sport).SetSubSport(sub).
		SetTotalElapsedTimeScaled(float64(n)).SetTotalTimerTimeScaled(float64(n)).
		SetAvgHeartRate(tot.avgHR()).SetMaxHeartRate(uint8(tot.maxHR))
}

// totals accumulate samples.
type totals struct {
	n                      int
	hr, power, dist, maxHR float64
}

func (t *totals) add(hr, watts, speed float64) {
	t.n++
	t.hr += hr
	t.power += watts
	t.dist += speed
	t.maxHR = math.Max(t.maxHR, hr)
}

func (t totals) avgHR() uint8 { return uint8(t.hr / float64(max(t.n, 1))) }

func (t totals) avgPower() uint16 { return uint16(t.power / float64(max(t.n, 1))) }

func semicircles(deg float64) int32 { return int32(deg * (1 << 31) / 180) }

func dur(seconds float64) time.Duration { return time.Duration(seconds * float64(time.Second)) }

func clamp(v, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }

func ptr[T any](v T) *T { return &v }
