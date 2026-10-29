package fit

import (
	"strings"

	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/profile/untyped/mesgnum"
	"github.com/muktihari/fit/proto"

	"github.com/iskyd/kudos/internal/importer"
)

func length(l *mesgdef.Length) importer.Length {
	return importer.Length{
		// The timestamp repeats the activity start; each length has its own start time.
		StartTime:   l.StartTime.UTC(),
		ElapsedTime: scaled(l.TotalElapsedTimeScaled()),
		TimerTime:   scaled(l.TotalTimerTimeScaled()),
		Active:      l.LengthType == typedef.LengthTypeActive,
		Stroke:      stroke(l.SwimStroke),
		Strokes:     num(l.TotalStrokes, basetype.Uint16Invalid),
		AvgSpeed:    scaled(l.AvgSpeedScaled()),
	}
}

func stroke(s typedef.SwimStroke) string {
	switch s {
	case typedef.SwimStrokeFreestyle, typedef.SwimStrokeBackstroke, typedef.SwimStrokeBreaststroke,
		typedef.SwimStrokeButterfly, typedef.SwimStrokeDrill, typedef.SwimStrokeMixed, typedef.SwimStrokeIm:
		return s.String()
	case typedef.SwimStrokeImByRound, typedef.SwimStrokeRimo:
		return typedef.SwimStrokeIm.String()
	}
	return ""
}

type exerciseKey struct {
	category typedef.ExerciseCategory
	number   uint16
}

// strengthSets reads sets and exercise titles, which the library leaves among unrelated messages.
func strengthSets(file *filedef.Activity) []*mesgdef.Set {
	var sets []*mesgdef.Set
	for i := range file.UnrelatedMessages {
		if file.UnrelatedMessages[i].Num == mesgnum.Set {
			sets = append(sets, mesgdef.NewSet(&file.UnrelatedMessages[i]))
		}
	}
	return sets
}

func exerciseNames(file *filedef.Activity) map[exerciseKey]string {
	names := map[exerciseKey]string{}
	for i := range file.UnrelatedMessages {
		if file.UnrelatedMessages[i].Num != mesgnum.ExerciseTitle {
			continue
		}
		t := mesgdef.NewExerciseTitle(&file.UnrelatedMessages[i])
		// The name decodes as a string array; the first element is the name.
		if len(t.WktStepName) > 0 && t.WktStepName[0] != "" {
			names[exerciseKey{t.ExerciseCategory, t.ExerciseName}] = t.WktStepName[0]
		}
	}
	return names
}

func set(s *mesgdef.Set, names map[exerciseKey]string) importer.Set {
	out := importer.Set{
		// The timestamp repeats the activity start; each set has its own start time.
		StartTime:        s.StartTime.UTC(),
		Duration:         scaled(s.DurationScaled()),
		Active:           s.SetType == typedef.SetTypeActive,
		Reps:             num(s.Repetitions, basetype.Uint16Invalid),
		Weight:           scaled(s.WeightScaled()),
		WorkoutStepIndex: messageIndex(s.WktStepIndex),
	}
	// Matched to its title by category and number, never by position.
	if len(s.Category) > 0 && len(s.CategorySubtype) > 0 &&
		s.Category[0] != typedef.ExerciseCategoryInvalid && s.CategorySubtype[0] != basetype.Uint16Invalid {
		out.ExerciseCategory, out.ExerciseNumber = ptr(int(s.Category[0])), ptr(int(s.CategorySubtype[0]))
		out.ExerciseName = names[exerciseKey{s.Category[0], s.CategorySubtype[0]}]
	}
	return out
}

func workout(file *filedef.Activity) *importer.Workout {
	if len(file.Workouts) == 0 {
		return nil
	}
	w := &importer.Workout{Name: file.Workouts[0].WktName}
	for _, s := range file.WorkoutSteps {
		w.Steps = append(w.Steps, workoutStep(s))
	}
	return w
}

// workoutStep interprets raw step values by their type: distance in cm, time in ms,
// speed in mm/s, heart rate as bpm + 100 (or % of max), power as W + 1000 (or % of FTP).
func workoutStep(s *mesgdef.WorkoutStep) importer.WorkoutStep {
	st := importer.WorkoutStep{
		Index:        int(s.MessageIndex & typedef.MessageIndexMask),
		Name:         s.WktStepName,
		Intensity:    intensity(s.Intensity),
		DurationType: s.DurationType.String(),
	}
	d := s.DurationValue
	if d != basetype.Uint32Invalid {
		switch s.DurationType {
		case typedef.WktStepDurationTime:
			st.Duration = ptr(float64(d) / 1000)
		case typedef.WktStepDurationDistance:
			st.Duration = ptr(float64(d) / 100)
		case typedef.WktStepDurationReps, typedef.WktStepDurationCalories:
			st.Duration = ptr(float64(d))
		case typedef.WktStepDurationRepeatUntilStepsCmplt:
			st.RepeatFrom = ptr(int(d))
			if s.TargetValue != basetype.Uint32Invalid {
				st.RepeatCount = ptr(int(s.TargetValue))
			}
			return st
		}
	}

	lo, hi := s.CustomTargetValueLow, s.CustomTargetValueHigh
	custom := lo != basetype.Uint32Invalid && hi != basetype.Uint32Invalid && (lo != 0 || hi != 0)
	switch s.TargetType {
	case typedef.WktStepTargetSpeed:
		st.TargetType = "speed"
		if custom {
			st.TargetLow, st.TargetHigh = ptr(float64(lo)/1000), ptr(float64(hi)/1000)
		}
	case typedef.WktStepTargetHeartRate:
		st.TargetType = "heart_rate"
		if custom {
			st.TargetType, st.TargetLow, st.TargetHigh = offsetTarget("heart_rate", lo, hi, 100)
		}
	case typedef.WktStepTargetPower:
		st.TargetType = "power"
		if custom {
			st.TargetType, st.TargetLow, st.TargetHigh = offsetTarget("power", lo, hi, 1000)
		}
	case typedef.WktStepTargetCadence:
		st.TargetType = "cadence"
		if custom {
			st.TargetLow, st.TargetHigh = ptr(float64(lo)), ptr(float64(hi))
		}
	default:
		return withExercise(st, s)
	}
	if !custom && s.TargetValue != basetype.Uint32Invalid && s.TargetValue != 0 {
		st.TargetZone = ptr(int(s.TargetValue))
	}
	return withExercise(st, s)
}

// offsetTarget decodes heart rate and power targets: values above the offset are
// absolute (value − offset), values up to it are a percentage.
func offsetTarget(kind string, lo, hi, offset uint32) (string, *float64, *float64) {
	if lo > offset && hi > offset {
		return kind, ptr(float64(lo - offset)), ptr(float64(hi - offset))
	}
	return kind + "_percent", ptr(float64(lo)), ptr(float64(hi))
}

func withExercise(st importer.WorkoutStep, s *mesgdef.WorkoutStep) importer.WorkoutStep {
	if s.ExerciseCategory != typedef.ExerciseCategoryInvalid && s.ExerciseName != basetype.Uint16Invalid {
		st.ExerciseCategory, st.ExerciseNumber = ptr(int(s.ExerciseCategory)), ptr(int(s.ExerciseName))
	}
	st.Weight = scaled(s.ExerciseWeightScaled())
	return st
}

// thresholds reads the time_in_zone message that refers to session i.
func thresholds(file *filedef.Activity, i int) *importer.Thresholds {
	for _, z := range file.TimeInZones {
		if z.ReferenceMesg != mesgnum.Session || int(z.ReferenceIndex&typedef.MessageIndexMask) != i {
			continue
		}
		t := &importer.Thresholds{
			FTP:         num(z.FunctionalThresholdPower, basetype.Uint16Invalid),
			ThresholdHR: num(z.ThresholdHeartRate, basetype.Uint8Invalid),
			MaxHR:       num(z.MaxHeartRate, basetype.Uint8Invalid),
			RestingHR:   num(z.RestingHeartRate, basetype.Uint8Invalid),
		}
		for _, b := range z.HrZoneHighBoundary {
			if v := num(b, basetype.Uint8Invalid); v != nil {
				t.HRZones = append(t.HRZones, *v)
			}
		}
		for _, b := range z.PowerZoneHighBoundary {
			if v := num(b, basetype.Uint16Invalid); v != nil {
				t.PowerZones = append(t.PowerZones, *v)
			}
		}
		if t.FTP == nil && t.ThresholdHR == nil && t.MaxHR == nil && t.RestingHR == nil && t.HRZones == nil && t.PowerZones == nil {
			return nil
		}
		return t
	}
	return nil
}

type devKey struct{ index, num uint8 }

// devField is a developer field's name and scaling, from its field description.
type devField struct {
	name          string
	baseType      basetype.BaseType
	scale, offset float64
}

// devFields resolves developer fields by name through their descriptions.
func devFields(file *filedef.Activity) map[devKey]devField {
	fields := map[devKey]devField{}
	for _, d := range file.FieldDescriptions {
		if len(d.FieldName) == 0 || d.FieldName[0] == "" {
			continue
		}
		f := devField{name: strings.ToLower(d.FieldName[0]), baseType: d.FitBaseTypeId, scale: 1}
		// Zwift declares the invalid sentinels (255, 127) as scale and offset: treat them as unset.
		if d.Scale != basetype.Uint8Invalid && d.Scale != 0 {
			f.scale = float64(d.Scale)
		}
		if d.Offset != basetype.Sint8Invalid {
			f.offset = float64(d.Offset)
		}
		// Known fields get a unit-suffixed name like the other extras.
		if f.name == "target_power" {
			f.name = "target_power_w"
		}
		fields[devKey{d.DeveloperDataIndex, d.FieldDefinitionNumber}] = f
	}
	return fields
}

// addDevFields puts valid developer values into the sample's extras.
func addDevFields(s *importer.Sample, dev []proto.DeveloperField, fields map[devKey]devField) {
	for _, d := range dev {
		f, ok := fields[devKey{d.DeveloperDataIndex, d.Num}]
		if !ok || !d.Value.Valid(f.baseType) {
			continue
		}
		v, ok := float(d.Value)
		if !ok {
			continue
		}
		if s.Extras == nil {
			s.Extras = map[string]float64{}
		}
		s.Extras[f.name] = v/f.scale - f.offset
	}
}

// float converts a scalar numeric value; arrays and strings are skipped.
func float(v proto.Value) (float64, bool) {
	switch x := v.Any().(type) {
	case int8:
		return float64(x), true
	case uint8:
		return float64(x), true
	case int16:
		return float64(x), true
	case uint16:
		return float64(x), true
	case int32:
		return float64(x), true
	case uint32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func messageIndex(i typedef.MessageIndex) *int {
	if i == typedef.MessageIndexInvalid {
		return nil
	}
	return ptr(int(i & typedef.MessageIndexMask))
}

func ptr[T any](v T) *T { return &v }
