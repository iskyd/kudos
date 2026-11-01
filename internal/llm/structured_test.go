package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// scripted replies with its replies in order and records the requests.
type scripted struct {
	replies []string
	errs    []error
	got     []Request
}

func (s *scripted) Complete(_ context.Context, req Request) (string, error) {
	i := len(s.got)
	s.got = append(s.got, req)
	if i < len(s.errs) && s.errs[i] != nil {
		return "", s.errs[i]
	}
	return s.replies[i], nil
}

func (*scripted) Stream(context.Context, Request, func(string) error) error {
	return errors.New("not used")
}

const workoutSchema = `{
	"type": "object",
	"required": ["name", "minutes"],
	"properties": {
		"name": {"type": "string", "minLength": 1},
		"minutes": {"type": "integer", "minimum": 1}
	},
	"additionalProperties": false
}`

func schema(t *testing.T) *Schema {
	t.Helper()
	s, err := CompileSchema("workout.json", []byte(workoutSchema))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStructuredValidFirstTime(t *testing.T) {
	t.Parallel()
	for name, reply := range map[string]string{
		"bare":   `{"name": "Easy run", "minutes": 40}`,
		"fenced": "```json\n{\"name\": \"Easy run\", \"minutes\": 40}\n```",
		"prose":  "Here is the workout:\n{\"name\": \"Easy run\", \"minutes\": 40}\nEnjoy!",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := &scripted{replies: []string{reply}}
			out, err := Structured(t.Context(), c, conversation, schema(t))
			if err != nil || string(out) != `{"name": "Easy run", "minutes": 40}` || len(c.got) != 1 {
				t.Errorf("out %s, err %v, %d calls", out, err, len(c.got))
			}
		})
	}
}

func TestStructuredRetriesOnceWithTheErrors(t *testing.T) {
	t.Parallel()
	c := &scripted{replies: []string{`{"name": "Easy run", "minutes": "forty"}`, `{"name": "Easy run", "minutes": 40}`}}
	out, err := Structured(t.Context(), c, conversation, schema(t))
	if err != nil || string(out) != `{"name": "Easy run", "minutes": 40}` {
		t.Fatalf("out %s, err %v", out, err)
	}
	retry := c.got[1]
	if retry.System != conversation.System || len(retry.Messages) != 3 || len(conversation.Messages) != 1 {
		t.Fatalf("retry = %+v", retry)
	}
	if m := retry.Messages[1]; m.Role != "assistant" || m.Content != `{"name": "Easy run", "minutes": "forty"}` {
		t.Errorf("retry shows %+v", m)
	}
	if m := retry.Messages[2]; m.Role != "user" || !strings.Contains(m.Content, "/minutes") || !strings.Contains(m.Content, "integer") {
		t.Errorf("retry asks %q", m.Content)
	}
}

func TestStructuredFailsAfterTheRetry(t *testing.T) {
	t.Parallel()
	for name, replies := range map[string][]string{
		"schema twice":     {`{"name": ""}`, `{"name": "Run", "minutes": 0}`},
		"not JSON twice":   {"I cannot do that.", "{unfinished"},
		"extra field then": {`{"name": "Run", "minutes": 5, "pace": 1}`, "no"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := &scripted{replies: replies}
			if out, err := Structured(t.Context(), c, conversation, schema(t)); !errors.Is(err, ErrInvalidOutput) || out != nil || len(c.got) != 2 {
				t.Errorf("out %s, err %v, %d calls", out, err, len(c.got))
			}
		})
	}
}

func TestStructuredClientErrors(t *testing.T) {
	t.Parallel()
	down := &APIError{Status: 503, Message: "down"}
	for name, c := range map[string]*scripted{
		"first call": {errs: []error{down}},
		"retry call": {replies: []string{"nope"}, errs: []error{nil, down}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Structured(t.Context(), c, conversation, schema(t)); !errors.Is(err, down) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestCompileSchemaErrors(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]string{
		"not JSON":       `{"type":`,
		"invalid schema": `{"type": 42}`,
	} {
		if _, err := CompileSchema("bad.json", []byte(doc)); err == nil || !strings.Contains(err.Error(), "bad.json") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestExtractJSON(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`[1, 2]`:                     `[1, 2]`,
		"```\n[1]\n```":              `[1]`,
		"```json":                    "```json",
		"no json here":               "no json here",
		"} backwards {":              "} backwards {",
		"  {\"a\": {\"b\": 1}} ok  ": `{"a": {"b": 1}}`,
	} {
		if got := extractJSON(in); got != want {
			t.Errorf("extractJSON(%q) = %q, want %q", in, got, want)
		}
	}
}
