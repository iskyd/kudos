package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// Kind is an error kind. Its problem type, urn:kudos:problem:<kind>, is a stable
// contract with clients: never rename a kind.
type Kind string

// Error kinds. Add a kind here, in status and in title together.
const (
	KindInvalidRequest   Kind = "invalid-request"
	KindNotFound         Kind = "not-found"
	KindMethodNotAllowed Kind = "method-not-allowed"
	KindInternal         Kind = "internal"

	KindUnauthorized         Kind = "unauthorized"
	KindInvalidCredentials   Kind = "invalid-credentials" //nolint:gosec // G101: an error kind name, not a credential
	KindCSRF                 Kind = "csrf"
	KindCrossOrigin          Kind = "cross-origin"
	KindRegistrationDisabled Kind = "registration-disabled"
	KindUsernameTaken        Kind = "username-taken"

	KindUnsupportedFile Kind = "unsupported-file"
	KindInvalidFile     Kind = "invalid-file"
	KindFileTooLarge    Kind = "file-too-large"

	KindGarminKeyRejected Kind = "garmin-key-rejected"
	KindGarminUnavailable Kind = "garmin-unavailable"
	KindBackfillExists    Kind = "backfill-exists"
)

func (k Kind) status() int {
	switch k {
	case KindInvalidRequest:
		return http.StatusBadRequest
	case KindUnauthorized, KindInvalidCredentials:
		return http.StatusUnauthorized
	case KindCSRF, KindCrossOrigin, KindRegistrationDisabled:
		return http.StatusForbidden
	case KindUsernameTaken, KindBackfillExists:
		return http.StatusConflict
	case KindNotFound:
		return http.StatusNotFound
	case KindMethodNotAllowed:
		return http.StatusMethodNotAllowed
	case KindUnsupportedFile:
		return http.StatusUnsupportedMediaType
	case KindInvalidFile, KindGarminKeyRejected:
		return http.StatusUnprocessableEntity
	case KindGarminUnavailable:
		return http.StatusBadGateway
	case KindFileTooLarge:
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusInternalServerError
	}
}

func (k Kind) title() string {
	switch k {
	case KindInvalidRequest:
		return "Invalid request"
	case KindNotFound:
		return "Not found"
	case KindMethodNotAllowed:
		return "Method not allowed"
	case KindUnauthorized:
		return "Not logged in"
	case KindInvalidCredentials:
		return "Invalid username or password"
	case KindCSRF:
		return "Missing or invalid CSRF token"
	case KindCrossOrigin:
		return "Cross-origin request rejected"
	case KindRegistrationDisabled:
		return "Registration is disabled"
	case KindUsernameTaken:
		return "Username already taken"
	case KindUnsupportedFile:
		return "Unsupported file type"
	case KindInvalidFile:
		return "File could not be read"
	case KindFileTooLarge:
		return "File too large"
	case KindGarminKeyRejected:
		return "Garmin rejected the key"
	case KindGarminUnavailable:
		return "Garmin is unavailable"
	case KindBackfillExists:
		return "Backfill already started"
	default:
		return "Internal error"
	}
}

// Error is an error that renders as a problem of its kind. Detail is shown to
// the client, so it must never contain secrets or user data.
type Error struct {
	Kind   Kind
	Detail string
}

func (e *Error) Error() string { return string(e.Kind) + ": " + e.Detail }

// writeProblem renders err as application/problem+json. Errors that are not an
// *Error are logged and rendered as an internal error without detail.
func writeProblem(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var e *Error
	if !errors.As(err, &e) {
		logger.ErrorContext(r.Context(), "request failed", "request_id", middleware.GetReqID(r.Context()), "error", err)
		e = &Error{Kind: KindInternal}
	}
	if e.Kind.status() == http.StatusInternalServerError {
		e = &Error{Kind: KindInternal}
	}

	p := Problem{
		Type:   "urn:kudos:problem:" + string(e.Kind),
		Title:  e.Kind.title(),
		Status: e.Kind.status(),
	}
	if e.Detail != "" {
		p.Detail = &e.Detail
	}
	if id := middleware.GetReqID(r.Context()); id != "" {
		p.RequestId = &id
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p) // the status is already sent; nothing left to report to
}
