package secret

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const plain = "hunter2-very-secret"

func TestRedaction(t *testing.T) {
	t.Parallel()

	s := New(plain)
	holder := struct{ Key Value[string] }{Key: s}

	tests := []struct {
		name   string
		render func() string
	}{
		{name: "slog json attr", render: func() string {
			var buf bytes.Buffer
			slog.New(slog.NewJSONHandler(&buf, nil)).Info("msg", "key", s)
			return buf.String()
		}},
		{name: "slog json struct", render: func() string {
			var buf bytes.Buffer
			slog.New(slog.NewJSONHandler(&buf, nil)).Info("msg", "cfg", holder)
			return buf.String()
		}},
		{name: "slog text attr", render: func() string {
			var buf bytes.Buffer
			slog.New(slog.NewTextHandler(&buf, nil)).Info("msg", "key", s)
			return buf.String()
		}},
		{name: "fmt %v", render: func() string { return fmt.Sprintf("%v", s) }},
		{name: "fmt %q", render: func() string { return fmt.Sprintf("%q", s) }},
		{name: "fmt %+v struct", render: func() string { return fmt.Sprintf("%+v", holder) }},
		{name: "fmt %#v struct", render: func() string { return fmt.Sprintf("%#v", holder) }},
		{name: "json", render: func() string {
			b, err := json.Marshal(holder)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.render()
			if strings.Contains(got, plain) {
				t.Fatalf("secret leaked: %s", got)
			}
			if !strings.Contains(got, redacted) {
				t.Fatalf("missing %s marker: %s", redacted, got)
			}
		})
	}
}

func TestReveal(t *testing.T) {
	t.Parallel()

	if got := New(plain).Reveal(); got != plain {
		t.Fatalf("Reveal() = %q, want %q", got, plain)
	}
	if got := New([]byte{1, 2}).Reveal(); !bytes.Equal(got, []byte{1, 2}) {
		t.Fatalf("Reveal() = %v, want [1 2]", got)
	}
}
