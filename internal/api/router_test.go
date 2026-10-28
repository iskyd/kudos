package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	var p Problem
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decoding problem: %v", err)
	}
	return p
}

func TestRouter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
		wantBody string
		wantType string
	}{
		{name: "healthz ok", method: http.MethodGet, path: "/healthz", wantCode: http.StatusOK, wantBody: "ok"},
		{name: "method not allowed", method: http.MethodPost, path: "/healthz", wantCode: http.StatusMethodNotAllowed, wantType: "urn:kudos:problem:method-not-allowed"},
		{name: "unknown route", method: http.MethodGet, path: "/nope", wantCode: http.StatusNotFound, wantType: "urn:kudos:problem:not-found"},
	}

	h := NewRouter(discard())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil))

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if rec.Header().Get("X-Request-Id") == "" {
				t.Fatal("missing X-Request-Id header")
			}
			if tt.wantType == "" {
				if rec.Body.String() != tt.wantBody {
					t.Fatalf("body = %q, want %q", rec.Body.String(), tt.wantBody)
				}
				return
			}
			p := decodeProblem(t, rec)
			if p.Type != tt.wantType || p.Status != tt.wantCode {
				t.Fatalf("problem = %+v, want type %s status %d", p, tt.wantType, tt.wantCode)
			}
			if p.RequestId == nil || *p.RequestId != rec.Header().Get("X-Request-Id") {
				t.Fatalf("problem requestId = %v, want header value", p.RequestId)
			}
		})
	}
}

func TestAccessLogOmitsQueryAndBody(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewRouter(slog.New(slog.NewJSONHandler(&buf, nil)))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz?token=s3cret", strings.NewReader("body-s3cret"))
	h.ServeHTTP(httptest.NewRecorder(), req)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("access log is not one JSON line: %v: %s", err, buf.String())
	}
	if strings.Contains(buf.String(), "s3cret") {
		t.Fatalf("access log leaked query or body: %s", buf.String())
	}
	for key, want := range map[string]any{"msg": "request", "method": "GET", "path": "/healthz", "status": 200.0, "bytes": 2.0} {
		if line[key] != want {
			t.Errorf("%s = %v, want %v", key, line[key], want)
		}
	}
	if line["request_id"] == "" || line["request_id"] == nil {
		t.Error("missing request_id")
	}
}
