package fit

import (
	"bytes"
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"

	"github.com/iskyd/kudos/internal/importer"
)

func TestPoolSwimLengthsAndSWOLF(t *testing.T) {
	t.Parallel()
	a := decodeFile(t, filepath.Join(corpus, "pool_swim.fit"))[0]
	if a.Sport != importer.SportPoolSwimming || a.PoolLength == nil || *a.PoolLength != 50 || len(a.Lengths) != 29 {
		t.Fatalf("pool swim: sport %s pool %v lengths %d; want pool_swimming, 50 m, 29", a.Sport, deref(a.PoolLength), len(a.Lengths))
	}
	first := a.Lengths[1]
	if !first.Active || first.Stroke != "freestyle" || *first.Strokes != 31 || *first.SWOLF() != 72.75+31 {
		t.Errorf("first active length = %+v, SWOLF %v; want freestyle, 31 strokes, SWOLF 103.75", first, deref(first.SWOLF()))
	}
	// Each length has its own start time; the timestamp field repeats the activity start.
	if !first.StartTime.Equal(time.Date(2025, 11, 19, 12, 7, 23, 0, time.UTC)) {
		t.Errorf("first active length starts %v", first.StartTime)
	}
	active, distance := 0, 0.0
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, l := range a.Lengths {
		swolf := l.SWOLF()
		if !l.Active {
			if swolf != nil {
				t.Errorf("idle length at %v has SWOLF %v", l.StartTime, *swolf)
			}
			continue
		}
		active++
		distance += *a.PoolLength
		lo, hi = min(lo, *swolf), max(hi, *swolf)
	}
	if active != 20 || distance != *a.Distance {
		t.Errorf("%d active lengths, %v m; want 20 and the session's %v m", active, distance, *a.Distance)
	}
	// Fastest length: 62.251 s, 27 strokes; slowest: 93.687 s, 37 strokes.
	if math.Abs(lo-89.251) > 1e-9 || math.Abs(hi-130.687) > 1e-9 {
		t.Errorf("SWOLF range %v–%v, want 89.251–130.687", lo, hi)
	}
}

func TestStrengthSets(t *testing.T) {
	t.Parallel()
	a := decodeFile(t, filepath.Join(corpus, "strength.fit"))[0]
	if a.Sport != importer.SportStrength || len(a.Laps) != 0 || len(a.Sets) != 54 || a.RPE != nil || a.Feel != nil {
		t.Fatalf("strength: sport %s laps %d sets %d rpe %v feel %v", a.Sport, len(a.Laps), len(a.Sets), deref(a.RPE), deref(a.Feel))
	}
	first, plank := a.Sets[0], a.Sets[2]
	if !first.Active || *first.Reps != 10 || *first.Weight != 0 || *first.Duration != 40 || *first.WorkoutStepIndex != 0 {
		t.Errorf("first set = %+v", first)
	}
	if !plank.StartTime.Equal(time.Date(2026, 3, 23, 15, 42, 56, 0, time.UTC)) || plank.ExerciseName != "Side Plank" ||
		*plank.ExerciseCategory != int(typedef.ExerciseCategoryPlank) || *plank.ExerciseNumber != 66 {
		t.Errorf("third set = %+v; want Side Plank (plank/66) starting 15:42:56", plank)
	}
	if rest := a.Sets[1]; rest.Active || rest.ExerciseCategory != nil || rest.Reps != nil {
		t.Errorf("rest set = %+v", rest)
	}
	if a.Workout == nil || a.Workout.Name != "Strength Workout" || len(a.Workout.Steps) != 19 {
		t.Fatalf("workout = %+v", a.Workout)
	}
	if st := a.Workout.Steps[10]; st.DurationType != "reps" || *st.Duration != 6 || *st.ExerciseCategory != int(typedef.ExerciseCategoryPushUp) {
		t.Errorf("push-up step = %+v", st)
	}
}

func TestExerciseNamesMatchByCategoryAndNumber(t *testing.T) {
	t.Parallel()
	title := func(cat typedef.ExerciseCategory, n uint16, name string) proto.Message {
		return mesgdef.NewExerciseTitle(nil).SetExerciseCategory(cat).SetExerciseName(n).SetWktStepName([]string{name, "\x01"}).ToMesg(nil)
	}
	set := func(cat typedef.ExerciseCategory, n uint16) proto.Message {
		return mesgdef.NewSet(nil).SetSetType(typedef.SetTypeActive).SetCategory([]typedef.ExerciseCategory{cat, cat}).SetCategorySubtype([]uint16{n, n}).ToMesg(nil)
	}
	// Titles appear in a different order from the sets that use them.
	file := filedef.NewActivity(
		title(typedef.ExerciseCategoryPlank, 66, "Side Plank"),
		title(typedef.ExerciseCategoryPlank, 43, "Plank"),
		title(typedef.ExerciseCategoryPushUp, 77, "Push-up"),
		set(typedef.ExerciseCategoryPushUp, 77), set(typedef.ExerciseCategoryPlank, 43), set(typedef.ExerciseCategoryPlank, 66),
		set(typedef.ExerciseCategoryLunge, 48),
	)
	names := exerciseNames(file)
	var got []string
	for _, s := range strengthSets(file) {
		got = append(got, set2name(s, names))
	}
	if want := []string{"Push-up", "Plank", "Side Plank", ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("names = %q, want %q", got, want)
	}
}

func set2name(s *mesgdef.Set, names map[exerciseKey]string) string { return set(s, names).ExerciseName }

func TestRecordedWorkoutIntervals(t *testing.T) {
	t.Parallel()
	a := decodeFile(t, filepath.Join(corpus, "intervals.fit"))[0]
	if a.Workout == nil || a.Workout.Name != "ripetute brevi" {
		t.Fatalf("workout = %+v", a.Workout)
	}
	want := []importer.WorkoutStep{
		{Index: 0, Intensity: "warmup", DurationType: "distance", Duration: ptr(2000.0)},
		{Index: 1, Intensity: "active", DurationType: "distance", Duration: ptr(400.0), TargetType: "speed", TargetLow: ptr(4.167), TargetHigh: ptr(4.348)},
		{Index: 2, Intensity: "recovery", DurationType: "time", Duration: ptr(60.0)},
		{Index: 3, DurationType: "repeat_until_steps_cmplt", RepeatFrom: ptr(1), RepeatCount: ptr(10)},
		{Index: 4, Intensity: "cooldown", DurationType: "distance", Duration: ptr(2000.0)},
	}
	if !reflect.DeepEqual(a.Workout.Steps, want) {
		t.Errorf("steps = %+v\nwant    %+v", a.Workout.Steps, want)
	}
	// The 2 km warm-up was recorded as two 1 km laps of step 0.
	var steps []int
	for _, l := range a.Laps[:4] {
		steps = append(steps, *l.WorkoutStepIndex)
	}
	if !reflect.DeepEqual(steps, []int{0, 0, 1, 2}) {
		t.Errorf("first lap steps = %v, want [0 0 1 2]", steps)
	}
	if *a.RPE != 7 || *a.Feel != 25 {
		t.Errorf("rpe %v feel %v; want 7 and 25", *a.RPE, *a.Feel)
	}
}

func TestThresholdsPerSample(t *testing.T) {
	t.Parallel()
	watchHR := func(thr, maxHR float64, zones ...float64) *importer.Thresholds {
		return &importer.Thresholds{ThresholdHR: ptr(thr), MaxHR: ptr(maxHR), RestingHR: ptr(58.0), HRZones: zones}
	}
	run := watchHR(171, 187, 97, 116, 135, 154, 174, 187)
	run.FTP, run.PowerZones = ptr(378.0), []float64{270, 333, 377, 418, 480, 4000}
	tests := map[string]*importer.Thresholds{
		"half_marathon": run,
		"intervals":     run,
		"pool_swim":     watchHR(176, 194, 97, 116, 136, 155, 175, 194),
		"strength":      watchHR(175, 192, 96, 115, 134, 154, 173, 192),
		"bike":          watchHR(185, 205, 97, 116, 135, 154, 174, 205),
		"hike":          watchHR(185, 205, 97, 116, 135, 154, 174, 205),
		"zwift":         nil, // third-party files carry none
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := decodeFile(t, filepath.Join(corpus, name+".fit"))[0].Thresholds; !reflect.DeepEqual(got, want) {
				t.Errorf("thresholds = %+v, want %+v", got, want)
			}
		})
	}
}

func TestZwiftDeveloperFields(t *testing.T) {
	t.Parallel()
	a := decodeFile(t, filepath.Join(corpus, "zwift.fit"))[0]
	withTarget := 0
	for _, s := range a.Samples {
		for k := range s.Extras {
			// The CORE fields Zwift declares are always empty and must be dropped.
			if k != "target_power_w" {
				t.Fatalf("sample at %v has extra %q", s.Time, k)
			}
		}
		if _, ok := s.Extras["target_power_w"]; ok {
			withTarget++
		}
	}
	if withTarget < len(a.Samples)*9/10 {
		t.Errorf("%d of %d samples have target power; want nearly all (per-second)", withTarget, len(a.Samples))
	}
}

func TestDevFieldsScaling(t *testing.T) {
	t.Parallel()
	desc := func(num uint8, name string, bt basetype.BaseType, scale uint8, offset int8) *mesgdef.FieldDescription {
		return mesgdef.NewFieldDescription(nil).SetDeveloperDataIndex(0).SetFieldDefinitionNumber(num).
			SetFieldName([]string{name}).SetFitBaseTypeId(bt).SetScale(scale).SetOffset(offset)
	}
	file := &filedef.Activity{FieldDescriptions: []*mesgdef.FieldDescription{
		desc(0, "Target_Power", basetype.Uint16, basetype.Uint8Invalid, basetype.Sint8Invalid),
		desc(1, "core_temp", basetype.Uint16, 100, 0),
		desc(2, "offset_value", basetype.Sint8, 1, 10),
		desc(3, "", basetype.Uint8, 1, 0),
		desc(4, "label", basetype.String, 1, 0),
	}}
	fields := devFields(file)
	var s importer.Sample
	addDevFields(&s, []proto.DeveloperField{
		{Num: 0, Value: proto.Uint16(250)},
		{Num: 1, Value: proto.Uint16(3712)},
		{Num: 2, Value: proto.Int8(15)},
		{Num: 1, DeveloperDataIndex: 9, Value: proto.Uint16(1)}, // no description
		{Num: 3, Value: proto.Uint8(1)},                         // nameless description
		{Num: 4, Value: proto.String("x")},                      // not a number
		{Num: 2, Value: proto.Int8(basetype.Sint8Invalid)},      // sentinel
	}, fields)
	want := map[string]float64{"target_power_w": 250, "core_temp": 37.12, "offset_value": 5}
	if !reflect.DeepEqual(s.Extras, want) {
		t.Errorf("extras = %v, want %v", s.Extras, want)
	}
}

func TestFloat(t *testing.T) {
	t.Parallel()
	for _, v := range []proto.Value{
		proto.Int8(2), proto.Uint8(2), proto.Int16(2), proto.Uint16(2), proto.Int32(2), proto.Uint32(2),
		proto.Int64(2), proto.Uint64(2), proto.Float32(2), proto.Float64(2),
	} {
		if f, ok := float(v); !ok || f != 2 {
			t.Errorf("float(%v) = %v, %v", v.Any(), f, ok)
		}
	}
	if _, ok := float(proto.SliceUint8([]uint8{1})); ok {
		t.Error("arrays must be skipped")
	}
}

func TestWorkoutStepTargets(t *testing.T) {
	t.Parallel()
	step := func(target typedef.WktStepTarget, value, lo, hi uint32) *mesgdef.WorkoutStep {
		return mesgdef.NewWorkoutStep(nil).SetMessageIndex(3).SetDurationType(typedef.WktStepDurationOpen).
			SetTargetType(target).SetTargetValue(value).SetCustomTargetValueLow(lo).SetCustomTargetValueHigh(hi)
	}
	tests := []struct {
		name              string
		step              *mesgdef.WorkoutStep
		wantType          string
		wantLow, wantHigh *float64
		wantZone          *int
	}{
		{"heart rate bpm", step(typedef.WktStepTargetHeartRate, 0, 250, 265), "heart_rate", ptr(150.0), ptr(165.0), nil},
		{"heart rate percent", step(typedef.WktStepTargetHeartRate, 0, 70, 80), "heart_rate_percent", ptr(70.0), ptr(80.0), nil},
		{"heart rate zone", step(typedef.WktStepTargetHeartRate, 3, 0, 0), "heart_rate", nil, nil, ptr(3)},
		{"power watts", step(typedef.WktStepTargetPower, 0, 1250, 1300), "power", ptr(250.0), ptr(300.0), nil},
		{"power percent", step(typedef.WktStepTargetPower, 0, 90, 105), "power_percent", ptr(90.0), ptr(105.0), nil},
		{"cadence", step(typedef.WktStepTargetCadence, 0, 85, 95), "cadence", ptr(85.0), ptr(95.0), nil},
		{"speed zone", step(typedef.WktStepTargetSpeed, 2, basetype.Uint32Invalid, basetype.Uint32Invalid), "speed", nil, nil, ptr(2)},
		{"open", step(typedef.WktStepTargetOpen, 0, basetype.Uint32Invalid, basetype.Uint32Invalid), "", nil, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := workoutStep(tt.step)
			if got.Index != 3 || got.DurationType != "open" || got.Duration != nil || got.TargetType != tt.wantType ||
				!reflect.DeepEqual(got.TargetLow, tt.wantLow) || !reflect.DeepEqual(got.TargetHigh, tt.wantHigh) || !reflect.DeepEqual(got.TargetZone, tt.wantZone) {
				t.Errorf("step = %+v", got)
			}
		})
	}

	cal := workoutStep(mesgdef.NewWorkoutStep(nil).SetDurationType(typedef.WktStepDurationCalories).SetDurationValue(300).
		SetTargetType(typedef.WktStepTargetOpen).SetExerciseCategory(typedef.ExerciseCategorySquat).SetExerciseName(5).SetExerciseWeight(2250))
	if *cal.Duration != 300 || *cal.ExerciseCategory != int(typedef.ExerciseCategorySquat) || *cal.ExerciseNumber != 5 || *cal.Weight != 22.5 {
		t.Errorf("calories step = %+v", cal)
	}
}

func TestStroke(t *testing.T) {
	t.Parallel()
	for in, want := range map[typedef.SwimStroke]string{
		typedef.SwimStrokeBreaststroke: "breaststroke", typedef.SwimStrokeIm: "im", typedef.SwimStrokeImByRound: "im",
		typedef.SwimStrokeRimo: "im", typedef.SwimStrokeInvalid: "",
	} {
		if got := stroke(in); got != want {
			t.Errorf("stroke(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestThresholdsMissing(t *testing.T) {
	t.Parallel()
	empty := mesgdef.NewTimeInZone(nil).SetReferenceMesg(typedef.MesgNumSession).SetReferenceIndex(0)
	lap := mesgdef.NewTimeInZone(nil).SetReferenceMesg(typedef.MesgNumLap).SetReferenceIndex(0).SetMaxHeartRate(190)
	if got := thresholds(&filedef.Activity{TimeInZones: []*mesgdef.TimeInZone{lap, empty}}, 0); got != nil {
		t.Errorf("all-invalid session zones = %+v, want nil", got)
	}
	if got := thresholds(&filedef.Activity{TimeInZones: []*mesgdef.TimeInZone{lap}}, 0); got != nil {
		t.Errorf("lap-only zones = %+v, want nil", got)
	}
}

func TestRPE(t *testing.T) {
	t.Parallel()
	if got := rpe(65); got == nil || math.Abs(*got-6.5) > 1e-12 {
		t.Errorf("rpe(65) = %v, want 6.5", deref(got))
	}
	if rpe(basetype.Uint8Invalid) != nil {
		t.Error("rpe(255) must be nil")
	}
}

// triathlon is a synthetic multisport file: no real sample exists.
func triathlon(t testing.TB) []byte {
	t0 := time.Date(2026, 6, 7, 7, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	mesgs := []proto.Message{
		mesgdef.NewFileId(nil).SetType(typedef.FileActivity).SetManufacturer(typedef.ManufacturerGarmin).SetTimeCreated(t0).ToMesg(nil),
	}
	legs := []struct {
		sport typedef.Sport
		sub   typedef.SubSport
		start int
	}{
		{typedef.SportSwimming, typedef.SubSportOpenWater, 0},
		{typedef.SportTransition, typedef.SubSportGeneric, 100},
		{typedef.SportCycling, typedef.SubSportRoad, 200},
		{typedef.SportTransition, typedef.SubSportGeneric, 300},
		{typedef.SportRunning, typedef.SubSportGeneric, 400},
	}
	for i, leg := range legs {
		for s := leg.start; s < leg.start+3; s++ {
			mesgs = append(mesgs, mesgdef.NewRecord(nil).SetTimestamp(at(s)).SetHeartRate(uint8(120+i)).SetCadence(80).ToMesg(nil))
		}
		mesgs = append(mesgs,
			mesgdef.NewLap(nil).SetMessageIndex(typedef.MessageIndex(i)).SetTimestamp(at(leg.start+2)).SetStartTime(at(leg.start)).ToMesg(nil),
			mesgdef.NewSession(nil).SetMessageIndex(typedef.MessageIndex(i)).SetTimestamp(at(leg.start+2)).SetStartTime(at(leg.start)).
				SetSport(leg.sport).SetSubSport(leg.sub).ToMesg(nil),
		)
	}
	mesgs = append(mesgs, mesgdef.NewActivity(nil).SetTimestamp(at(402)).SetLocalTimestamp(at(402).Add(2*time.Hour)).SetNumSessions(5).ToMesg(nil))
	return encode(t, mesgs...)
}

func TestTriathlonSplitsIntoLegs(t *testing.T) {
	t.Parallel()
	acts, err := Decode(bytes.NewReader(triathlon(t)))
	if err != nil {
		t.Fatal(err)
	}
	want := []importer.Sport{importer.SportOpenWaterSwimming, importer.SportTransition, importer.SportCycling, importer.SportTransition, importer.SportRunning}
	if len(acts) != len(want) {
		t.Fatalf("got %d activities, want %d", len(acts), len(want))
	}
	for i, a := range acts {
		if a.Sport != want[i] || len(a.Laps) != 1 || len(a.Samples) != 3 || *a.Samples[0].HR != float64(120+i) ||
			a.UTCOffset == nil || *a.UTCOffset != 2*time.Hour {
			t.Errorf("leg %d = %s with %d laps, %d samples, offset %v", i, a.Sport, len(a.Laps), len(a.Samples), a.UTCOffset)
		}
	}
	// Only the run leg records cadence in strides.
	if *acts[4].Samples[0].Cadence != 160 || *acts[2].Samples[0].Cadence != 80 {
		t.Errorf("cadence run %v, bike %v; want 160 and 80", *acts[4].Samples[0].Cadence, *acts[2].Samples[0].Cadence)
	}
}

func TestWorkoutAttachment(t *testing.T) {
	t.Parallel()
	step := func(i int) *int { return &i }
	acts := []importer.Activity{{}, {Laps: []importer.Lap{{WorkoutStepIndex: step(0)}}}, {Sets: []importer.Set{{WorkoutStepIndex: step(1)}}}}
	if usesSteps(acts[0]) || !usesSteps(acts[1]) || !usesSteps(acts[2]) {
		t.Error("usesSteps should see lap and set step indices only")
	}
	// A workout no lap points at still lands on the first session.
	b := encode(t,
		mesgdef.NewFileId(nil).SetType(typedef.FileActivity).ToMesg(nil),
		mesgdef.NewWorkout(nil).SetWktName("tempo").ToMesg(nil),
		mesgdef.NewSession(nil).SetStartTime(time.Unix(1_800_000_000, 0)).SetSport(typedef.SportRunning).ToMesg(nil),
	)
	got, err := Decode(bytes.NewReader(b))
	if err != nil || got[0].Workout == nil || got[0].Workout.Name != "tempo" {
		t.Fatalf("Decode = %+v, %v", got, err)
	}
}
