package analytics

import (
	"math"
	"testing"
)

// bounds lists each zone's name and its [low, high) bounds, with nil as -1.
func bounds(zs []Zone) []any {
	v := func(p *float64) float64 {
		if p == nil {
			return -1
		}
		return math.Round(*p*1000) / 1000
	}
	out := make([]any, 0, 3*len(zs))
	for _, z := range zs {
		out = append(out, z.Name, v(z.Low), v(z.High))
	}
	return out
}

func sameBounds(t *testing.T, name string, got []Zone, want []any) {
	t.Helper()
	g := bounds(got)
	if len(g) != len(want) {
		t.Fatalf("%s: %v, want %v", name, g, want)
	}
	for i := range g {
		if g[i] != want[i] {
			t.Errorf("%s: %v, want %v", name, g, want)
			return
		}
	}
}

// Thresholds of 100 turn bounds into the tables' percentages.
func TestZoneModelsMatchPublishedTables(t *testing.T) {
	t.Parallel()
	hundred := new(100.0)

	// Friel running HR: Z1 < 85%, Z2 85–89, Z3 90–94, Z4 95–99, Z5a 100–102, Z5b 103–106, Z5c > 106.
	z := ZonesFor(ZoneRunning, Thresholds{LTHR: hundred}, Thresholds{})
	if z.HRModel != ModelFrielRunHR {
		t.Errorf("run HR model %q", z.HRModel)
	}
	sameBounds(t, "Friel run HR", z.HR, []any{
		"Z1", -1.0, 85.0, "Z2", 85.0, 90.0, "Z3", 90.0, 95.0, "Z4", 95.0, 100.0,
		"Z5a", 100.0, 103.0, "Z5b", 103.0, 107.0, "Z5c", 107.0, -1.0,
	})

	// Friel cycling HR: Z1 < 81%, Z2 81–89, Z3 90–93, Z4 94–99, Z5a 100–102, Z5b 103–106, Z5c > 106.
	// Coggan: ≤ 55%, 56–75, 76–90, 91–105, 106–120, 121–150, > 150.
	z = ZonesFor(ZoneCycling, Thresholds{LTHR: hundred, FTP: hundred}, Thresholds{})
	sameBounds(t, "Friel bike HR", z.HR, []any{
		"Z1", -1.0, 81.0, "Z2", 81.0, 90.0, "Z3", 90.0, 94.0, "Z4", 94.0, 100.0,
		"Z5a", 100.0, 103.0, "Z5b", 103.0, 107.0, "Z5c", 107.0, -1.0,
	})
	if z.PowerModel != ModelCoggan || z.Pace != nil {
		t.Errorf("cycling models: %+v", z)
	}
	sameBounds(t, "Coggan", z.Power, []any{
		"Active recovery", -1.0, 56.0, "Endurance", 56.0, 76.0, "Tempo", 76.0, 91.0,
		"Threshold", 91.0, 106.0, "VO2max", 106.0, 121.0, "Anaerobic", 121.0, 151.0, "Neuromuscular", 151.0, -1.0,
	})
}

func TestPaceZones(t *testing.T) {
	t.Parallel()
	// Friel running pace from a 4:00/km threshold: Z1 slower than 129% of it
	// (5:12/km starts Z2), Z2 114–129%, Z3 106–113%, Z4 101–105%, Z5a 97–100%,
	// Z5b 90–96%, Z5c faster than 90% (3:36/km).
	z := ZonesFor(ZoneRunning, Thresholds{ThresholdSpeed: new(1000 / 240.0)}, Thresholds{})
	if z.PaceModel != ModelFrielRunPace || z.HR != nil {
		t.Errorf("running models: %+v", z)
	}
	paces := []float64{312, 273.6, 254.4, 242.4, 232.8, 216} // s/km at each bound
	for i, p := range paces {
		near(t, "Friel pace bound", z.Pace[i].High, 1000/p)
		near(t, "Friel pace bound", z.Pace[i+1].Low, 1000/p)
	}
	if z.Pace[0].Name != "Z1" || z.Pace[0].Low != nil || z.Pace[6].High != nil {
		t.Errorf("open ends: %+v", z.Pace)
	}

	// CSS 1:40/100 m: Recovery slower than 1:50, Easy 1:46–1:50, Steady 1:43–1:46,
	// Threshold 1:38–1:43, Fast faster than 1:38.
	z = ZonesFor(ZoneSwimming, Thresholds{CSS: new(1.0)}, Thresholds{})
	if z.PaceModel != ModelCSS || z.Pace[0].Name != "Recovery" || z.Pace[4].Name != "Fast" {
		t.Errorf("swim zones: %+v", z)
	}
	for i, p := range []float64{110, 106, 103, 98} { // s/100 m at each bound
		near(t, "CSS bound", z.Pace[i].High, 100/p)
	}
}

func TestHeartRateReserveFallback(t *testing.T) {
	t.Parallel()
	// Max 190, resting 50: reserve 140, so 60/70/80/90% fall at 134, 148, 162, 176 bpm.
	general := Thresholds{MaxHR: new(190.0), RestingHR: new(50.0)}
	want := []any{"Z1", -1.0, 134.0, "Z2", 134.0, 148.0, "Z3", 148.0, 162.0, "Z4", 162.0, 176.0, "Z5", 176.0, -1.0}

	// Hiking has no model of its own: the general version applies.
	z := ZonesFor(ZoneGeneral, Thresholds{}, general)
	if z.HRModel != ModelHRR {
		t.Errorf("model %q", z.HRModel)
	}
	sameBounds(t, "HRR from general", z.HR, want)
	// A swim has no LTHR model either.
	sameBounds(t, "HRR for a swim", ZonesFor(ZoneSwimming, Thresholds{CSS: new(1.0)}, general).HR, want)
	// A sport's own max and resting HR win over the general version's.
	own := ZonesFor(ZoneCycling, Thresholds{MaxHR: new(150.0), RestingHR: new(50.0)}, general)
	near(t, "own HRR", own.HR[0].High, 110)
	// LTHR wins over both.
	if z := ZonesFor(ZoneRunning, Thresholds{LTHR: new(171.0)}, general); z.HRModel != ModelFrielRunHR {
		t.Errorf("model %q", z.HRModel)
	}
	if z := ZonesFor(ZoneGeneral, Thresholds{}, Thresholds{MaxHR: new(190.0)}); z.HR != nil {
		t.Errorf("zones without resting HR: %+v", z.HR)
	}
}

func TestNoThresholdCrossesSports(t *testing.T) {
	t.Parallel()
	// A running-power FTP (378 W in the samples) and a threshold pace never give
	// another sport's zones, and swims get no power or running pace zones.
	all := Thresholds{FTP: new(378.0), ThresholdSpeed: new(4.0), CSS: new(1.2)}
	for sport, z := range map[ZoneSport]Zones{
		ZoneRunning:  ZonesFor(ZoneRunning, all, Thresholds{}),
		ZoneSwimming: ZonesFor(ZoneSwimming, all, Thresholds{}),
		ZoneGeneral:  ZonesFor(ZoneGeneral, all, Thresholds{}),
	} {
		if z.Power != nil {
			t.Errorf("%s got power zones", sport)
		}
	}
	if z := ZonesFor(ZoneCycling, all, Thresholds{}); z.Pace != nil {
		t.Errorf("cycling got pace zones: %+v", z.Pace)
	}
	if z := ZonesFor(ZoneGeneral, all, Thresholds{}); z.Pace != nil {
		t.Errorf("general got pace zones: %+v", z.Pace)
	}
}

func TestZoneSportOf(t *testing.T) {
	t.Parallel()
	for sport, want := range map[string]ZoneSport{
		"running": ZoneRunning, "trail_running": ZoneRunning, "cycling": ZoneCycling,
		"pool_swimming": ZoneSwimming, "open_water_swimming": ZoneSwimming,
		"hiking": ZoneGeneral, "strength": ZoneGeneral, "other": ZoneGeneral,
	} {
		if got := ZoneSportOf(sport); got != want {
			t.Errorf("ZoneSportOf(%q) = %q, want %q", sport, got, want)
		}
	}
}

func TestTimeInZones(t *testing.T) {
	t.Parallel()
	zs := zones([]string{"easy", "hard"}, []float64{150})
	hr := func(s Sample) *float64 { return s.HR }
	// 1 s at 140, a 5 s smart-recording gap at 160, a 60 s pause at 140 (not
	// counted), 2 s at 170 in a second segment; samples without HR count nowhere.
	got := TimeInZones([][]Sample{
		{
			{Time: at(0), HR: new(140.0)},
			{Time: at(1), HR: new(160.0)},
			{Time: at(6), HR: new(140.0)},
			{Time: at(66), HR: new(150.0)},
			{Time: at(67)},
			{Time: at(69), HR: new(140.0)},
		},
		{{Time: at(100), HR: new(170.0)}, {Time: at(102), HR: new(120.0)}},
	}, zs, hr)
	// easy: 1 s; hard: 5 s + 1 s (150 starts hard) + 2 s.
	if len(got) != 2 || got[0] != 1 || got[1] != 8 {
		t.Errorf("TimeInZones = %v, want [1 8]", got)
	}
	if TimeInZones(nil, nil, hr) != nil {
		t.Error("time without zones")
	}
}
