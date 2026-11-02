package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Schema is a compiled JSON Schema that structured outputs are checked against.
type Schema struct {
	s *jsonschema.Schema
}

// CompileSchema compiles a JSON Schema document; name identifies it in errors.
func CompileSchema(name string, doc []byte) (*Schema, error) {
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return nil, fmt.Errorf("reading schema %s: %w", name, err)
	}
	c := jsonschema.NewCompiler()
	url := "kudos:///" + name
	if err := c.AddResource(url, v); err != nil {
		return nil, fmt.Errorf("adding schema %s: %w", name, err)
	}
	s, err := c.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("compiling schema %s: %w", name, err)
	}
	return &Schema{s: s}, nil
}

// ErrInvalidOutput means the model's output failed validation on both tries.
var ErrInvalidOutput = errors.New("LLM output failed validation")

// Structured asks for JSON matching schema. Invalid output gets one retry that
// shows the model its reply and the validation errors; a second failure is
// ErrInvalidOutput. Only validated output is returned (invariant 33).
func Structured(ctx context.Context, c Client, req Request, schema *Schema) (json.RawMessage, error) {
	reply, err := c.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	out, invalid := schema.check(reply)
	if invalid == nil {
		return out, nil
	}
	retry := req
	retry.Messages = append(slices.Clone(req.Messages),
		Message{Role: "assistant", Content: reply},
		Message{Role: "user", Content: "That reply is invalid: " + invalid.Error() + "\nReply again with only the corrected JSON."},
	)
	if reply, err = c.Complete(ctx, retry); err != nil {
		return nil, err
	}
	if out, invalid = schema.check(reply); invalid != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidOutput, invalid)
	}
	return out, nil
}

// check extracts the JSON from a reply and validates it.
func (s *Schema) check(reply string) (json.RawMessage, error) {
	text := extractJSON(reply)
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(text))
	if err != nil {
		return nil, fmt.Errorf("not JSON: %w", err)
	}
	if err := s.s.Validate(v); err != nil {
		return nil, fmt.Errorf("does not match the schema: %w", err)
	}
	return json.RawMessage(text), nil
}

// extractJSON drops what models wrap JSON in: Markdown fences and prose around it.
func extractJSON(reply string) string {
	s := strings.TrimSpace(reply)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	start := strings.IndexAny(s, "{[")
	end := strings.LastIndexAny(s, "}]")
	if start < 0 || end < start {
		return s
	}
	return s[start : end+1]
}
