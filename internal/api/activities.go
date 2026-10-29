package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"time"
	"unicode/utf8"

	"github.com/iskyd/kudos/internal/importer"
	"github.com/iskyd/kudos/internal/importer/ingest"
)

// Keep in step with api/openapi.yaml.
const (
	maxUploadBytes   = 64 << 20
	maxDurationS     = 7 * 24 * 3600
	maxDistanceM     = 10_000_000
	maxNotesLen      = 2000
	localStartLayout = "2006-01-02T15:04"
)

// UploadActivity stores the activities of an uploaded FIT, GPX or TCX file.
func (s *Server) UploadActivity(ctx context.Context, req UploadActivityRequestObject) (UploadActivityResponseObject, error) {
	sess := sessionFrom(ctx)
	name, data, err := readFilePart(req.Body)
	if err != nil {
		return nil, err
	}
	ids, err := s.ingest.Upload(ctx, sess.userID, location(sess.timezone), name, data)
	switch {
	case errors.Is(err, ingest.ErrUnsupportedFormat):
		return nil, &Error{Kind: KindUnsupportedFile, Detail: err.Error()}
	case errors.Is(err, ingest.ErrInvalidFile):
		return nil, &Error{Kind: KindInvalidFile, Detail: err.Error()}
	case err != nil:
		return nil, fmt.Errorf("ingesting upload: %w", err)
	}
	return UploadActivity201JSONResponse{ActivityIds: ids}, nil
}

// readFilePart returns the name and content of the "file" field, read up to the size limit.
func readFilePart(r *multipart.Reader) (string, []byte, error) {
	for {
		part, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			return "", nil, &Error{Kind: KindInvalidRequest, Detail: "missing file field"}
		}
		if err != nil {
			return "", nil, &Error{Kind: KindInvalidRequest, Detail: "malformed multipart body"}
		}
		if part.FormName() != "file" {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, maxUploadBytes+1))
		if err != nil {
			return "", nil, &Error{Kind: KindInvalidRequest, Detail: "malformed multipart body"}
		}
		if len(data) > maxUploadBytes {
			return "", nil, &Error{Kind: KindFileTooLarge, Detail: "files are limited to 64 MiB"}
		}
		return part.FileName(), data, nil
	}
}

// CreateActivity stores a manual activity.
func (s *Server) CreateActivity(ctx context.Context, req CreateActivityRequestObject) (CreateActivityResponseObject, error) {
	sess := sessionFrom(ctx)
	b := req.Body
	invalid := func(detail string) (CreateActivityResponseObject, error) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: detail}
	}
	if !b.Sport.Valid() {
		return invalid("unknown sport")
	}
	start, err := time.ParseInLocation(localStartLayout, b.LocalStartTime, location(sess.timezone))
	if err != nil {
		return invalid("localStartTime must be YYYY-MM-DDTHH:MM")
	}
	if !(b.DurationS > 0 && b.DurationS <= maxDurationS) {
		return invalid("durationS must be over 0 and at most 7 days")
	}
	if b.DistanceM != nil && !(*b.DistanceM >= 0 && *b.DistanceM <= maxDistanceM) {
		return invalid("distanceM must be 0 to 10000 km")
	}
	if b.Rpe != nil && !(*b.Rpe >= 0 && *b.Rpe <= 10) {
		return invalid("rpe must be 0 to 10")
	}
	var notes string
	if b.Notes != nil {
		notes = *b.Notes
	}
	if utf8.RuneCountInString(notes) > maxNotesLen {
		return invalid(fmt.Sprintf("notes are limited to %d characters", maxNotesLen))
	}

	id, err := s.ingest.CreateManual(ctx, sess.userID, ingest.Manual{
		Sport: importer.Sport(b.Sport), Start: start, Duration: b.DurationS, Distance: b.DistanceM, RPE: b.Rpe, Notes: notes,
	})
	if err != nil {
		return nil, fmt.Errorf("creating manual activity: %w", err)
	}
	return CreateActivity201JSONResponse{Id: id}, nil
}

// location is the user's timezone; it was validated when set, so UTC is only a guard.
func location(tz string) *time.Location {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}
