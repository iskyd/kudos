package api

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecoverer(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	h := recoverer(slog.New(slog.NewJSONHandler(&logs, nil)))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Type != "urn:kudos:problem:internal" || p.Detail != nil {
		t.Fatalf("problem = %+v", p)
	}
	if !strings.Contains(logs.String(), `"panic":"boom"`) || !strings.Contains(logs.String(), "stack") {
		t.Fatalf("panic not logged with stack: %s", logs.String())
	}
}

func TestRecovererPassesThrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantPanic bool
	}{
		{name: "no panic", handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }},
		{name: "abort handler re-panics", handler: func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }, wantPanic: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			defer func() {
				rvr := recover()
				if (rvr != nil) != tt.wantPanic {
					t.Fatalf("recovered %v, wantPanic %v", rvr, tt.wantPanic)
				}
				if err, ok := rvr.(error); ok && !errors.Is(err, http.ErrAbortHandler) {
					t.Fatalf("re-panicked with %v", err)
				}
				if !tt.wantPanic && rec.Code != http.StatusTeapot {
					t.Fatalf("status = %d, want 418", rec.Code)
				}
			}()
			recoverer(discard())(tt.handler).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		})
	}
}
