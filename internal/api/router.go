// Package api holds the HTTP handlers, auth and the OpenAPI server.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server implements the generated StrictServerInterface.
type Server struct{}

var _ StrictServerInterface = Server{}

// NewRouter returns the root HTTP handler. Unmatched GET and HEAD requests outside
// /api go to spa, so the frontend owns every path the spec does not define.
func NewRouter(logger *slog.Logger, spa http.Handler) http.Handler {
	problem := func(err error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { writeProblem(w, r, logger, err) }
	}
	invalid := func(w http.ResponseWriter, r *http.Request, err error) {
		writeProblem(w, r, logger, &Error{Kind: KindInvalidRequest, Detail: err.Error()})
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID, accessLog(logger), recoverer(logger))
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

	strict := NewStrictHandlerWithOptions(Server{}, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  invalid,
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) { writeProblem(w, r, logger, err) },
	})
	return HandlerWithOptions(strict, ChiServerOptions{BaseRouter: r, ErrorHandlerFunc: invalid})
}

// GetHealthz is the liveness probe.
func (Server) GetHealthz(context.Context, GetHealthzRequestObject) (GetHealthzResponseObject, error) {
	return GetHealthz200TextResponse("ok"), nil
}
