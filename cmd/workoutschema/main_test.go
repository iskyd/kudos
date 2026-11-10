package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const spec = `openapi: 3.0.3
components:
  schemas:
    Unused:
      type: string
    Sport:
      type: string
      enum: [running, cycling]
    Workout:
      type: object
      required: [sport, steps]
      properties:
        day:
          type: string
          format: date
        sport:
          $ref: "#/components/schemas/Sport"
        steps:
          type: array
          items:
            $ref: "#/components/schemas/Step"
    Step:
      type: object
      example: {kind: warmup}
      properties:
        duration:
          type: number
          format: double
        steps:
          type: array
          items:
            $ref: "#/components/schemas/Step"
`

func TestRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in, out := filepath.Join(dir, "spec.yaml"), filepath.Join(dir, "out.json")
	if err := os.WriteFile(in, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(in, "Workout", out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$ref":    "#/$defs/Workout",
		"$defs": map[string]any{
			"Sport": map[string]any{"type": "string", "enum": []any{"running", "cycling"}},
			"Workout": map[string]any{
				"type": "object", "required": []any{"sport", "steps"},
				"properties": map[string]any{
					"day":   map[string]any{"type": "string", "format": "date"},
					"sport": map[string]any{"$ref": "#/$defs/Sport"},
					"steps": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/$defs/Step"}},
				},
			},
			"Step": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"duration": map[string]any{"type": "number"},
					"steps":    map[string]any{"type": "array", "items": map[string]any{"$ref": "#/$defs/Step"}},
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %s", b)
	}
	// The same spec always gives the same bytes.
	again := filepath.Join(dir, "again.json")
	if err := run(in, "Workout", again); err != nil {
		t.Fatal(err)
	}
	if b2, _ := os.ReadFile(again); string(b2) != string(b) {
		t.Error("output is not stable")
	}
}

func TestRunErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in := filepath.Join(dir, "spec.yaml")
	if err := os.WriteFile(in, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("components: [unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][3]string{
		"missing spec":   {filepath.Join(dir, "none.yaml"), "Workout", filepath.Join(dir, "o.json")},
		"invalid yaml":   {bad, "Workout", filepath.Join(dir, "o.json")},
		"unknown schema": {in, "Plan", filepath.Join(dir, "o.json")},
		"unwritable out": {in, "Workout", filepath.Join(dir, "no", "dir", "o.json")},
	} {
		if err := run(args[0], args[1], args[2]); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
