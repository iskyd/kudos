package plans

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/iskyd/kudos/internal/llm"
)

// workoutSchema is PlannedWorkoutInput from api/openapi.yaml as a JSON Schema
// (see generate.go), so the API and the LLM share one definition.
//
//go:embed workout.schema.json
var workoutSchema []byte

const (
	// maxRepeatDepth lets a repeat hold one more level of repeats.
	maxRepeatDepth = 2
	// maxUnrolledSteps bounds a workout once its repeats are written out.
	maxUnrolledSteps = 200
)

// The rules a JSON Schema cannot say are checked on this shape of the same JSON.
type step struct {
	Kind         string   `json:"kind"`
	DurationType string   `json:"durationType"`
	Duration     *float64 `json:"duration"`
	Target       *struct {
		Zone *int     `json:"zone"`
		Low  *float64 `json:"low"`
		High *float64 `json:"high"`
	} `json:"target"`
	Count *int   `json:"count"`
	Steps []step `json:"steps"`
}

// ValidateWorkout checks a planned workout's JSON against the schema, then the
// rules the schema cannot say: what a repeat holds, how deep repeats nest, when
// a step needs a duration and what makes a target. Errors name the step by its
// path (steps/1/steps/0), so a model can correct it.
func ValidateWorkout(doc []byte) error {
	s, err := llm.CompileSchema("planned-workout", workoutSchema)
	if err != nil {
		return err
	}
	if err := s.Validate(doc); err != nil {
		return err
	}
	var w struct {
		Steps []step `json:"steps"`
	}
	if err := json.Unmarshal(doc, &w); err != nil {
		return fmt.Errorf("reading workout: %w", err)
	}
	n, err := checkSteps(w.Steps, "steps", 0)
	if err != nil {
		return err
	}
	if n > maxUnrolledSteps {
		return fmt.Errorf("the workout has %d steps once its repeats are written out, more than %d", n, maxUnrolledSteps)
	}
	return nil
}

// checkSteps checks steps at a repeat depth and returns how many steps they
// make once repeats are written out.
func checkSteps(steps []step, path string, depth int) (int, error) {
	total := 0
	for i, s := range steps {
		at := fmt.Sprintf("%s/%d", path, i)
		if s.Kind == "repeat" {
			switch {
			case depth+1 > maxRepeatDepth:
				return 0, fmt.Errorf("%s: repeats nest at most %d deep", at, maxRepeatDepth)
			case s.Count == nil || len(s.Steps) == 0:
				return 0, fmt.Errorf("%s: a repeat needs count and steps", at)
			case s.DurationType != "" || s.Duration != nil || s.Target != nil:
				return 0, fmt.Errorf("%s: a repeat has only count and steps", at)
			}
			n, err := checkSteps(s.Steps, at+"/steps", depth+1)
			if err != nil {
				return 0, err
			}
			total += n * *s.Count
			continue
		}
		switch {
		case s.Count != nil || s.Steps != nil:
			return 0, fmt.Errorf("%s: only a repeat has count and steps", at)
		case s.DurationType == "":
			return 0, fmt.Errorf("%s: durationType is required", at)
		case s.DurationType == "open" && s.Duration != nil:
			return 0, fmt.Errorf("%s: an open step has no duration", at)
		case s.DurationType != "open" && s.Duration == nil:
			return 0, fmt.Errorf("%s: a %s step needs a duration", at, s.DurationType)
		}
		if t := s.Target; t != nil {
			switch {
			case t.Zone != nil && (t.Low != nil || t.High != nil):
				return 0, fmt.Errorf("%s: a target is a zone or a low-to-high range, not both", at)
			case t.Zone == nil && (t.Low == nil || t.High == nil):
				return 0, fmt.Errorf("%s: a target needs a zone, or both low and high", at)
			case t.Zone == nil && (*t.Low <= 0 || *t.Low > *t.High):
				return 0, fmt.Errorf("%s: a target range needs 0 < low <= high", at)
			}
		}
		total++
	}
	return total, nil
}
