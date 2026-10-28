// Package secret wraps sensitive values so they never leak through logs, fmt or JSON.
package secret

import "log/slog"

const redacted = "[redacted]"

// Value holds a sensitive value. Every textual rendering prints [redacted];
// only Reveal returns the underlying value.
type Value[T any] struct {
	v T
}

// New wraps v.
func New[T any](v T) Value[T] {
	return Value[T]{v: v}
}

// Reveal returns the wrapped value. Call it only where the value is used.
func (s Value[T]) Reveal() T {
	return s.v
}

// String implements fmt.Stringer.
func (Value[T]) String() string { return redacted }

// GoString implements fmt.GoStringer so %#v is redacted too.
func (Value[T]) GoString() string { return redacted }

// LogValue implements slog.LogValuer.
func (Value[T]) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalJSON implements json.Marshaler.
func (Value[T]) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
