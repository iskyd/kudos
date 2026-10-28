package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		method   string
		wantCode int
		wantBody string
	}{
		{name: "get returns ok", method: http.MethodGet, wantCode: http.StatusOK, wantBody: "ok"},
		{name: "post not allowed", method: http.MethodPost, wantCode: http.StatusMethodNotAllowed},
	}

	h := NewRouter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tt.method, "/healthz", nil))

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}
