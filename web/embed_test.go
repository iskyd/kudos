package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestSPA(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"index.html":      {Data: []byte("<html>app</html>")},
		"favicon.svg":     {Data: []byte("<svg/>")},
		"assets/app.js":   {Data: []byte("console.log(1)")},
		"assets/sub/x.js": {Data: []byte("x")},
	}

	tests := []struct {
		name      string
		path      string
		wantCode  int
		wantBody  string
		wantCache string
	}{
		{name: "root is index", path: "/", wantCode: 200, wantBody: "<html>app</html>", wantCache: "no-cache"},
		{name: "client route falls back to index", path: "/activities/42", wantCode: 200, wantBody: "<html>app</html>", wantCache: "no-cache"},
		{name: "directory falls back to index", path: "/assets/", wantCode: 200, wantBody: "<html>app</html>", wantCache: "no-cache"},
		{name: "asset is immutable", path: "/assets/app.js", wantCode: 200, wantBody: "console.log(1)", wantCache: "public, max-age=31536000, immutable"},
		{name: "public file", path: "/favicon.svg", wantCode: 200, wantBody: "<svg/>"},
		{name: "traversal is rejected", path: "/../index.html", wantCode: 400, wantBody: "invalid URL path\n"},
	}

	h := SPA(fsys)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, nil))

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if rec.Body.String() != tt.wantBody {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Fatalf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
		})
	}
}

func TestSPAWithoutBuild(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	SPA(fstest.MapFS{}).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandler(t *testing.T) {
	t.Parallel()

	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	// dist may or may not be built; either way the handler answers.
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 200 or 404", rec.Code)
	}
}
