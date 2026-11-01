package main

import (
	"io"
	"strings"
	"testing"
)

func TestRunValidatesArguments(t *testing.T) {
	t.Parallel()
	env := func(url string) func(string) string {
		return func(k string) string {
			if k == "DATABASE_URL" {
				return url
			}
			return ""
		}
	}
	for _, tt := range []struct {
		args []string
		url  string
		want string
	}{
		{nil, "postgres://x", "-user is required"},
		{[]string{"-user", "demo", "-years", "0"}, "postgres://x", "-years must be at least 1"},
		{[]string{"-unknown"}, "postgres://x", "flag provided but not defined"},
		{[]string{"-user", "demo"}, "", "DATABASE_URL is required"},
	} {
		err := run(t.Context(), tt.args, env(tt.url), io.Discard)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("run(%v) = %v, want %q", tt.args, err, tt.want)
		}
	}
}
