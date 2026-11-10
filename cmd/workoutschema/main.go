// Command workoutschema extracts a schema and every schema it refers to from
// the OpenAPI spec into a standalone JSON Schema (draft 2020-12), so the LLM's
// structured outputs are checked against the same definition as the API.
//
//	workoutschema api/openapi.yaml PlannedWorkoutInput out.schema.json
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

const refPrefix = "#/components/schemas/"

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: workoutschema spec.yaml Schema out.json")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2], os.Args[3]); err != nil {
		fmt.Fprintln(os.Stderr, "workoutschema:", err)
		os.Exit(1)
	}
}

func run(specPath, root, outPath string) error {
	b, err := os.ReadFile(specPath) //nolint:gosec // G304: the path comes from the go:generate line
	if err != nil {
		return fmt.Errorf("reading spec: %w", err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(b, &spec); err != nil {
		return fmt.Errorf("parsing spec: %w", err)
	}
	out, err := extract(spec.Components.Schemas, root)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outPath, out, 0o644); err != nil { //nolint:gosec // G306: a generated source file, committed
		return fmt.Errorf("writing schema: %w", err)
	}
	return nil
}

// extract returns root and the schemas it reaches as one JSON Schema document.
func extract(schemas map[string]any, root string) ([]byte, error) {
	defs := map[string]any{}
	var visit func(name string) error
	visit = func(name string) error {
		if _, done := defs[name]; done {
			return nil
		}
		s, ok := schemas[name]
		if !ok {
			return fmt.Errorf("no schema %q in the spec", name)
		}
		defs[name] = nil // marks it visited, so a schema that refers to itself stops here
		var refs []string
		defs[name] = rewrite(s, &refs)
		for _, r := range refs {
			if err := visit(r); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		return nil, err
	}
	doc := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$ref":    "#/$defs/" + root,
		"$defs":   defs,
	}
	// Maps marshal with sorted keys, so the output is stable.
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding schema: %w", err)
	}
	return append(b, '\n'), nil
}

// rewrite copies v, pointing component references at $defs and collecting
// their names. OpenAPI's number formats and examples are dropped: JSON Schema
// has no such formats.
func rewrite(v any, refs *[]string) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			switch s, _ := e.(string); {
			case k == "example":
				continue
			case k == "format" && (s == "double" || s == "float" || s == "int32" || s == "int64"):
				continue
			case k == "$ref" && strings.HasPrefix(s, refPrefix):
				name := strings.TrimPrefix(s, refPrefix)
				*refs = append(*refs, name)
				out[k] = "#/$defs/" + name
				continue
			}
			out[k] = rewrite(e, refs)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = rewrite(e, refs)
		}
		return out
	default:
		return v
	}
}
