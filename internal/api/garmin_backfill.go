package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/iskyd/kudos/internal/connectors/garminsync"
)

// Keep in step with api/openapi.yaml.
const (
	defaultBackfillDays = 120
	maxBackfillDays     = 365
	defaultPageSize     = 50
	maxPageSize         = 100
)

// GetGarminBackfill returns the backfill's progress.
func (s *Server) GetGarminBackfill(ctx context.Context, _ GetGarminBackfillRequestObject) (GetGarminBackfillResponseObject, error) {
	p, err := s.garmin.Sync.Progress(ctx, sessionFrom(ctx).userID)
	if errors.Is(err, garminsync.ErrNotFound) {
		return GetGarminBackfill200JSONResponse{Started: false}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the backfill: %w", err)
	}
	return GetGarminBackfill200JSONResponse(backfillProgress(p)), nil
}

// StartGarminBackfill queues the backfill of the last days local days.
func (s *Server) StartGarminBackfill(ctx context.Context, req StartGarminBackfillRequestObject) (StartGarminBackfillResponseObject, error) {
	sess := sessionFrom(ctx)
	days := defaultBackfillDays
	if req.Body.Days != nil {
		days = *req.Body.Days
	}
	if days < 1 || days > maxBackfillDays {
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("days must be 1 to %d", maxBackfillDays)}
	}
	today := localToday(sess.timezone)
	err := s.garmin.Sync.Start(ctx, sess.userID, today.AddDate(0, 0, 1-days), today)
	switch {
	case errors.Is(err, garminsync.ErrNotConnected):
		return nil, &Error{Kind: KindNotFound, Detail: "Garmin is not connected"}
	case errors.Is(err, garminsync.ErrExists):
		return nil, &Error{Kind: KindBackfillExists, Detail: "resume the existing backfill instead"}
	case err != nil:
		return nil, fmt.Errorf("starting the backfill: %w", err)
	}
	p, err := s.garmin.Sync.Progress(ctx, sess.userID)
	if err != nil {
		return nil, fmt.Errorf("reading the backfill: %w", err)
	}
	return StartGarminBackfill202JSONResponse(backfillProgress(p)), nil
}

// ResumeGarminBackfill continues a paused backfill, optionally retrying failures.
func (s *Server) ResumeGarminBackfill(ctx context.Context, req ResumeGarminBackfillRequestObject) (ResumeGarminBackfillResponseObject, error) {
	userID := sessionFrom(ctx).userID
	retry := req.Body.RetryFailed != nil && *req.Body.RetryFailed
	err := s.garmin.Sync.Resume(ctx, userID, retry)
	if errors.Is(err, garminsync.ErrNotFound) {
		return nil, &Error{Kind: KindNotFound, Detail: "no backfill was started"}
	}
	if err != nil {
		return nil, fmt.Errorf("resuming the backfill: %w", err)
	}
	p, err := s.garmin.Sync.Progress(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("reading the backfill: %w", err)
	}
	return ResumeGarminBackfill200JSONResponse(backfillProgress(p)), nil
}

// failureCursor is the position after the last failure of a page.
type failureCursor struct {
	Start time.Time `json:"s"`
	ID    string    `json:"i"`
}

// ListGarminBackfillFailures pages through the activities the backfill could not import.
func (s *Server) ListGarminBackfillFailures(ctx context.Context, req ListGarminBackfillFailuresRequestObject,
) (ListGarminBackfillFailuresResponseObject, error) {
	limit := defaultPageSize
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if limit < 1 || limit > maxPageSize {
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("limit must be 1 to %d", maxPageSize)}
	}
	var after failureCursor
	if req.Params.Cursor != nil {
		if err := s.cursors.Decode(*req.Params.Cursor, &after); err != nil {
			return nil, err
		}
	}
	// One extra row tells whether another page follows.
	rows, err := s.garmin.Sync.Failures(ctx, sessionFrom(ctx).userID, after.Start, after.ID, int32(limit+1))
	if err != nil {
		return nil, fmt.Errorf("listing backfill failures: %w", err)
	}
	page := GarminImportFailurePage{Items: []GarminImportFailure{}}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next, err := s.cursors.Encode(failureCursor{Start: last.StartTime, ID: last.SourceActivityID})
		if err != nil {
			return nil, err
		}
		page.NextCursor = &next
	}
	for _, r := range rows {
		reason := ""
		if r.Error != nil {
			reason = *r.Error
		}
		page.Items = append(page.Items, GarminImportFailure{SourceActivityId: r.SourceActivityID, StartTime: r.StartTime, Reason: reason})
	}
	return ListGarminBackfillFailures200JSONResponse(page), nil
}

func backfillProgress(p garminsync.Progress) GarminBackfill {
	b := p.Backfill
	status := GarminBackfillStatus(b.Status)
	daysTotal := int(b.WindowEnd.Sub(b.WindowStart).Hours()/24) + 1
	daysDone := 0
	if b.HealthThrough != nil {
		daysDone = int(b.HealthThrough.Sub(b.WindowStart).Hours()/24) + 1
	}
	total, done, failed := int(p.Activities.Total), int(p.Activities.Done), int(p.Activities.Failed)
	out := GarminBackfill{
		Started: true, Status: &status,
		WindowStart: &openapi_types.Date{Time: b.WindowStart}, WindowEnd: &openapi_types.Date{Time: b.WindowEnd},
		ActivitiesTotal: &total, ActivitiesDone: &done, ActivitiesFailed: &failed, DaysTotal: &daysTotal, DaysDone: &daysDone,
	}
	if b.PauseReason != nil {
		reason := GarminBackfillPauseReason(*b.PauseReason)
		out.PauseReason = &reason
	}
	return out
}
