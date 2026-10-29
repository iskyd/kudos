package api

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestWriteProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantType   string
		wantTitle  string
		wantDetail string
		wantLogged bool
	}{
		{name: "invalid request", err: &Error{Kind: KindInvalidRequest, Detail: "bad limit"}, wantStatus: 400, wantType: "urn:kudos:problem:invalid-request", wantTitle: "Invalid request", wantDetail: "bad limit"},
		{name: "not found", err: &Error{Kind: KindNotFound}, wantStatus: 404, wantType: "urn:kudos:problem:not-found", wantTitle: "Not found"},
		{name: "method not allowed", err: &Error{Kind: KindMethodNotAllowed}, wantStatus: 405, wantType: "urn:kudos:problem:method-not-allowed", wantTitle: "Method not allowed"},
		{name: "unsupported file", err: &Error{Kind: KindUnsupportedFile}, wantStatus: 415, wantType: "urn:kudos:problem:unsupported-file", wantTitle: "Unsupported file type"},
		{name: "invalid file", err: &Error{Kind: KindInvalidFile, Detail: "no session"}, wantStatus: 422, wantType: "urn:kudos:problem:invalid-file", wantTitle: "File could not be read", wantDetail: "no session"},
		{name: "file too large", err: &Error{Kind: KindFileTooLarge}, wantStatus: 413, wantType: "urn:kudos:problem:file-too-large", wantTitle: "File too large"},
		{name: "garmin key rejected", err: &Error{Kind: KindGarminKeyRejected}, wantStatus: 422, wantType: "urn:kudos:problem:garmin-key-rejected", wantTitle: "Garmin rejected the key"},
		{name: "garmin unavailable", err: &Error{Kind: KindGarminUnavailable}, wantStatus: 502, wantType: "urn:kudos:problem:garmin-unavailable", wantTitle: "Garmin is unavailable"},
		{name: "internal hides detail", err: &Error{Kind: KindInternal, Detail: "db password wrong"}, wantStatus: 500, wantType: "urn:kudos:problem:internal", wantTitle: "Internal error"},
		{name: "wrapped error keeps kind", err: fmt.Errorf("listing: %w", &Error{Kind: KindNotFound, Detail: "no such activity"}), wantStatus: 404, wantType: "urn:kudos:problem:not-found", wantTitle: "Not found", wantDetail: "no such activity"},
		{name: "plain error is internal and logged", err: errors.New("db password wrong"), wantStatus: 500, wantType: "urn:kudos:problem:internal", wantTitle: "Internal error", wantLogged: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeProblem(w, r, slog.New(slog.NewJSONHandler(&logs, nil)), tt.err)
			})).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			body := rec.Body.String()
			p := decodeProblem(t, rec)
			if p.Type != tt.wantType || p.Title != tt.wantTitle || p.Status != tt.wantStatus {
				t.Fatalf("problem = %+v", p)
			}
			gotDetail := ""
			if p.Detail != nil {
				gotDetail = *p.Detail
			}
			if gotDetail != tt.wantDetail {
				t.Fatalf("detail = %q, want %q", gotDetail, tt.wantDetail)
			}
			if p.RequestId == nil || *p.RequestId == "" {
				t.Fatal("missing requestId")
			}
			if strings.Contains(body, "password") {
				t.Fatalf("internal detail leaked: %s", body)
			}
			if logged := logs.Len() > 0; logged != tt.wantLogged {
				t.Fatalf("logged = %v, want %v: %s", logged, tt.wantLogged, logs.String())
			}
		})
	}
}

func TestErrorString(t *testing.T) {
	t.Parallel()

	if got := (&Error{Kind: KindNotFound, Detail: "x"}).Error(); got != "not-found: x" {
		t.Fatalf("Error() = %q", got)
	}
}
