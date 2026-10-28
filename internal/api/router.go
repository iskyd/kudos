// Package api holds the HTTP handlers, auth and the OpenAPI server.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// NewRouter returns the root HTTP handler.
func NewRouter() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", healthz)
	return r
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}
