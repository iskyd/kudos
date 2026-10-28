package api

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

type position struct {
	Start time.Time `json:"s"`
	ID    int64     `json:"i"`
}

func newCursors(t *testing.T, fill byte) *Cursors {
	t.Helper()
	cs, err := NewCursors(bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()

	cs := newCursors(t, 1)
	want := position{Start: time.Date(2026, 10, 28, 7, 30, 0, 0, time.UTC), ID: 42}
	s, err := cs.Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	var got position
	if err := cs.Decode(s, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Start.Equal(want.Start) || got.ID != want.ID {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if bytes.Contains([]byte(s), []byte("42")) {
		t.Fatalf("cursor is not opaque: %s", s)
	}
}

func TestCursorDecodeRejects(t *testing.T) {
	t.Parallel()

	cs := newCursors(t, 1)
	valid, err := cs.Encode(position{ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(valid)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	sealedNotJSON, err := cs.cipher.Seal([]byte("not json"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		cs     *Cursors
		cursor string
	}{
		{name: "tampered", cs: cs, cursor: base64.RawURLEncoding.EncodeToString(raw)},
		{name: "not base64", cs: cs, cursor: "!!!"},
		{name: "empty", cs: cs, cursor: ""},
		{name: "other key", cs: newCursors(t, 2), cursor: valid},
		{name: "not json", cs: cs, cursor: base64.RawURLEncoding.EncodeToString(sealedNotJSON)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var p position
			if err := tt.cs.Decode(tt.cursor, &p); !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("Decode() error = %v, want ErrInvalidCursor", err)
			}
		})
	}
}

func TestCursorEncodeError(t *testing.T) {
	t.Parallel()

	if _, err := newCursors(t, 1).Encode(make(chan int)); err == nil {
		t.Fatal("Encode(chan) succeeded, want error")
	}
}
