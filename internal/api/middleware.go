package api

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// accessLog logs one line per request. It never logs bodies or query strings,
// which may carry user data.
func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := middleware.GetReqID(r.Context())
			w.Header().Set("X-Request-Id", id)
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func() {
				logger.InfoContext(r.Context(), "request",
					"request_id", id,
					"method", r.Method,
					"path", r.URL.Path,
					"status", ww.Status(),
					"bytes", ww.BytesWritten(),
					"duration_ms", time.Since(start).Milliseconds(),
				)
			}()
			next.ServeHTTP(ww, r)
		})
	}
}

// recoverer turns a panic into a logged internal-error problem.
func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rvr := recover()
				if rvr == nil {
					return
				}
				if rvr == http.ErrAbortHandler { //nolint:errorlint // sentinel compared as net/http does
					panic(rvr)
				}
				logger.ErrorContext(r.Context(), "panic",
					"request_id", middleware.GetReqID(r.Context()),
					"panic", rvr,
					"stack", string(debug.Stack()),
				)
				writeProblem(w, r, logger, &Error{Kind: KindInternal})
			}()
			next.ServeHTTP(w, r)
		})
	}
}
