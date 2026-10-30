package analytics

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 7, 0, 0, 0, time.UTC)

func at(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }

func near(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, deref(got), want)
	}
}

func deref(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func TestSummarizeTimeWeights(t *testing.T) {
	t.Parallel()
	// 1 s, then a 4 s smart-recording gap, then a 60 s pause, then 2 s.
	seg := []Sample{
		{Time: at(0), HR: new(100.0), Distance: new(0.0), Power: new(200.0)},
		{Time: at(1), HR: new(110.0), Distance: new(3.0)},
		{Time: at(5), HR: new(150.0), Distance: new(15.0), Speed: new(3.0)},
		{Time: at(65), HR: new(90.0), Distance: new(15.0), Speed: new(5.0)},
		{Time: at(67), HR: new(200.0), Distance: new(21.0)},
	}
	s, ok := Summarize([][]Sample{seg})
	if !ok {
		t.Fatal("no summary")
	}
	if s.ElapsedS != 67 || s.TimerS != 7 || !s.Start.Equal(at(0)) || !s.End.Equal(at(67)) {
		t.Errorf("times: elapsed %v, timer %v, %v–%v", s.ElapsedS, s.TimerS, s.Start, s.End)
	}
	// (100·1 + 110·4 + 150·0 + 90·2) / 7; the last sample and the one before the pause weigh nothing.
	near(t, "avg HR", s.AvgHR, (100+440+180)/7.0)
	near(t, "max HR", s.MaxHR, 200)
	near(t, "distance", s.DistanceM, 21)
	// Distance over timer time wins over the speed channel.
	near(t, "avg speed", s.AvgSpeed, 3)
	near(t, "max speed", s.MaxSpeed, 5)
	// One power sample: its own value.
	near(t, "avg power", s.AvgPower, 200)
	if s.AvgCadence != nil || s.MaxCadence != nil || s.AvgTemperature != nil {
		t.Errorf("unrecorded channels: %+v", s)
	}
}

func TestSummarizeSegments(t *testing.T) {
	t.Parallel()
	// Two activities merged: distance restarts at 0 in the second, and the hour between them is not timer time.
	a := []Sample{{Time: at(0), Distance: new(0.0), Cadence: new(170.0)}, {Time: at(10), Distance: new(30.0), Cadence: new(180.0)}}
	b := []Sample{
		{Time: at(3600), Distance: new(0.0), Temperature: new(20.0)},
		{Time: at(3602), Distance: new(8.0), Temperature: new(22.0)},
		{Time: at(3603), Distance: new(10.0), Temperature: new(30.0)},
	}
	s, _ := Summarize([][]Sample{a, b})
	if s.ElapsedS != 3603 || s.TimerS != 13 {
		t.Errorf("elapsed %v, timer %v", s.ElapsedS, s.TimerS)
	}
	near(t, "distance", s.DistanceM, 40)
	near(t, "avg speed", s.AvgSpeed, 40/13.0)
	near(t, "avg cadence", s.AvgCadence, 170)
	near(t, "max cadence", s.MaxCadence, 180)
	near(t, "avg temperature", s.AvgTemperature, (20*2+22*1)/3.0)
}

func TestSummarizeSparse(t *testing.T) {
	t.Parallel()
	if _, ok := Summarize(nil); ok {
		t.Error("summary without samples")
	}
	if _, ok := Summarize([][]Sample{{}, {}}); ok {
		t.Error("summary of empty segments")
	}
	// Samples only separated by pauses: plain means; no distance, so speed comes from its channel.
	s, ok := Summarize([][]Sample{{{Time: at(0), HR: new(100.0), Speed: new(2.0)}, {Time: at(100), HR: new(120.0), Speed: new(4.0)}}})
	if !ok || s.TimerS != 0 || s.DistanceM != nil {
		t.Fatalf("summary = %+v", s)
	}
	near(t, "avg HR", s.AvgHR, 110)
	near(t, "avg speed", s.AvgSpeed, 3)
}
