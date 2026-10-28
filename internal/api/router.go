// Package api holds the HTTP handlers, auth and the OpenAPI server.
package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server implements the generated StrictServerInterface.
type Server struct{}

var _ StrictServerInterface = Server{}

// NewRouter returns the root HTTP handler.
func NewRouter(logger *slog.Logger) http.Handler {
	problem := func(err error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { writeProblem(w, r, logger, err) }
	}
	invalid := func(w http.ResponseWriter, r *http.Request, err error) {
		writeProblem(w, r, logger, &Error{Kind: KindInvalidRequest, Detail: err.Error()})
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID, accessLog(logger), recoverer(logger))
	r.NotFound(problem(&Error{Kind: KindNotFound}))
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
