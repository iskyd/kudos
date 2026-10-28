// Package api holds the HTTP handlers, auth and the OpenAPI server.
package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter returns the root HTTP handler. Unmatched GET and HEAD requests outside
// /api go to spa, so the frontend owns every path the spec does not define.
func NewRouter(logger *slog.Logger, srv *Server, spa http.Handler) http.Handler {
	problem := func(err error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { writeProblem(w, r, logger, err) }
	}
	invalid := func(w http.ResponseWriter, r *http.Request, err error) {
		writeProblem(w, r, logger, &Error{Kind: KindInvalidRequest, Detail: err.Error()})
	}

	// Rejects cross-site writes from browsers (Sec-Fetch-Site / Origin), covering
	// login and register, which have no session and so no CSRF token yet.
	crossOrigin := http.NewCrossOriginProtection()
	crossOrigin.SetDenyHandler(problem(&Error{Kind: KindCrossOrigin}))

	r := chi.NewRouter()
	r.Use(middleware.RequestID, accessLog(logger), recoverer(logger), crossOrigin.Handler, srv.loadSession(logger))
	notFound := problem(&Error{Kind: KindNotFound})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		isAPI := r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/")
		if !isAPI && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			spa.ServeHTTP(w, r)
			return
		}
		notFound(w, r)
	})
	r.MethodNotAllowed(problem(&Error{Kind: KindMethodNotAllowed}))

	strict := NewStrictHandlerWithOptions(srv, []StrictMiddlewareFunc{requireSession}, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  invalid,
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) { writeProblem(w, r, logger, err) },
	})
	return HandlerWithOptions(strict, ChiServerOptions{BaseRouter: r, ErrorHandlerFunc: invalid})
}
