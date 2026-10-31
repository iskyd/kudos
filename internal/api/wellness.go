package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/iskyd/kudos/internal/store/db"
)

// Keep in step with WellnessDayInput and HealthLogInput in api/openapi.yaml.
const maxWellnessText = 2000

var (
	errWellnessDayNotFound = &Error{Kind: KindNotFound, Detail: "nothing entered for that day"}
	errHealthLogNotFound   = &Error{Kind: KindNotFound, Detail: "no such log entry"}
)

// wellnessCursor is the last day of a page.
type wellnessCursor struct {
	Day time.Time `json:"d"`
}

// healthLogCursor is the position after a page's last entry.
type healthLogCursor struct {
	Start time.Time `json:"s"`
	ID    int64     `json:"i"`
}

// trimmed trims s to nil when nothing is left; ok is false over the length limit.
func trimmed(s *string) (out *string, ok bool) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil, true
	}
	t := strings.TrimSpace(*s)
	return &t, utf8.RuneCountInString(t) <= maxWellnessText
}

// within reports whether v is absent or in (lo, hi], or [lo, hi] when lo is allowed.
func within(v *float64, lo, hi float64, loAllowed bool) bool {
	return v == nil || ((*v > lo || loAllowed && *v == lo) && *v <= hi)
}

func wellnessDay(d db.WellnessDay) WellnessDay {
	return WellnessDay{
		Day: openapi_types.Date{Time: d.Day}, Soreness: intPtr(d.Soreness), SleepQuality: intPtr(d.SleepQuality),
		Stress: intPtr(d.Stress), Motivation: intPtr(d.Motivation), Note: d.Note,
		WeightKg: f64(d.WeightKg), BodyFatPct: f64(d.BodyFatPct), HydrationL: f64(d.HydrationL),
	}
}

// ListWellnessDays returns one page of the user's days, latest first.
func (s *Server) ListWellnessDays(ctx context.Context, req ListWellnessDaysRequestObject) (ListWellnessDaysResponseObject, error) {
	limit := defaultPageSize
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if limit < 1 || limit > maxPageSize {
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("limit must be 1 to %d", maxPageSize)}
	}
	args := db.ListWellnessDaysParams{UserID: sessionFrom(ctx).userID, MaxRows: int32(limit + 1)} // one extra row tells whether another page follows
	if req.Params.Cursor != nil {
		var c wellnessCursor
		if err := s.cursors.Decode(*req.Params.Cursor, &c); err != nil {
			return nil, err
		}
		args.Before = &c.Day
	}
	rows, err := s.q.ListWellnessDays(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("listing wellness days: %w", err)
	}
	page := WellnessDayPage{Items: []WellnessDay{}}
	if len(rows) > limit {
		rows = rows[:limit]
		next, err := s.cursors.Encode(wellnessCursor{Day: rows[limit-1].Day})
		if err != nil {
			return nil, err
		}
		page.NextCursor = &next
	}
	for _, r := range rows {
		page.Items = append(page.Items, wellnessDay(r))
	}
	return ListWellnessDays200JSONResponse(page), nil
}

// PutWellnessDay replaces the day's check-in and body metrics.
func (s *Server) PutWellnessDay(ctx context.Context, req PutWellnessDayRequestObject) (PutWellnessDayResponseObject, error) {
	b := req.Body
	invalid := func(detail string) (PutWellnessDayResponseObject, error) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: detail}
	}
	note, ok := trimmed(b.Note)
	switch {
	case !inRange(b.Soreness, 1, 5) || !inRange(b.SleepQuality, 1, 5) || !inRange(b.Stress, 1, 5) || !inRange(b.Motivation, 1, 5):
		return invalid("scores must be 1 to 5")
	case !ok:
		return invalid(fmt.Sprintf("note must be at most %d characters", maxWellnessText))
	case !within(b.WeightKg, 0, 500, false) || !within(b.BodyFatPct, 0, 100, true) || !within(b.HydrationL, 0, 20, true):
		return invalid("weightKg must be over 0 up to 500, bodyFatPct 0 to 100 and hydrationL 0 to 20")
	case b.Soreness == nil && b.SleepQuality == nil && b.Stress == nil && b.Motivation == nil && note == nil &&
		b.WeightKg == nil && b.BodyFatPct == nil && b.HydrationL == nil:
		return invalid("enter at least one value")
	}
	d, err := s.q.PutWellnessDay(ctx, db.PutWellnessDayParams{
		UserID: sessionFrom(ctx).userID, Day: req.Day.Time,
		Soreness: small(b.Soreness), SleepQuality: small(b.SleepQuality), Stress: small(b.Stress), Motivation: small(b.Motivation),
		Note: note, WeightKg: f32(b.WeightKg), BodyFatPct: f32(b.BodyFatPct), HydrationL: f32(b.HydrationL),
	})
	if err != nil {
		return nil, fmt.Errorf("saving wellness day: %w", err)
	}
	return PutWellnessDay200JSONResponse(wellnessDay(d)), nil
}

// DeleteWellnessDay deletes everything entered for the day.
func (s *Server) DeleteWellnessDay(ctx context.Context, req DeleteWellnessDayRequestObject) (DeleteWellnessDayResponseObject, error) {
	n, err := s.q.DeleteWellnessDay(ctx, db.DeleteWellnessDayParams{UserID: sessionFrom(ctx).userID, Day: req.Day.Time})
	if err != nil {
		return nil, fmt.Errorf("deleting wellness day: %w", err)
	}
	if n == 0 {
		return nil, errWellnessDayNotFound
	}
	return DeleteWellnessDay204Response{}, nil
}

func healthLogEntry(e db.HealthLog) HealthLogEntry {
	out := HealthLogEntry{Id: e.ID, Kind: HealthLogKind(e.Kind), Description: e.Description, StartDate: openapi_types.Date{Time: e.StartDate}}
	if e.EndDate != nil {
		out.EndDate = &openapi_types.Date{Time: *e.EndDate}
	}
	return out
}

// healthLogParams validates a create or update request and returns the stored values.
func healthLogParams(b *HealthLogInput) (db.UpdateHealthLogEntryParams, error) {
	invalid := func(detail string) (db.UpdateHealthLogEntryParams, error) {
		return db.UpdateHealthLogEntryParams{}, &Error{Kind: KindInvalidRequest, Detail: detail}
	}
	description, ok := trimmed(b.Description)
	switch {
	case !b.Kind.Valid():
		return invalid("kind must be injury, illness or period")
	case !ok:
		return invalid(fmt.Sprintf("description must be at most %d characters", maxWellnessText))
	case description == nil && b.Kind != Period:
		return invalid("describe the injury or illness")
	case b.EndDate != nil && b.EndDate.Before(b.StartDate.Time):
		return invalid("endDate must not be before startDate")
	}
	p := db.UpdateHealthLogEntryParams{Kind: string(b.Kind), Description: description, StartDate: b.StartDate.Time}
	if b.EndDate != nil {
		p.EndDate = &b.EndDate.Time
	}
	return p, nil
}

// ListHealthLog returns one page of the user's log, latest start first.
func (s *Server) ListHealthLog(ctx context.Context, req ListHealthLogRequestObject) (ListHealthLogResponseObject, error) {
	p := req.Params
	limit := defaultPageSize
	if p.Limit != nil {
		limit = *p.Limit
	}
	if limit < 1 || limit > maxPageSize {
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("limit must be 1 to %d", maxPageSize)}
	}
	args := db.ListHealthLogParams{UserID: sessionFrom(ctx).userID, MaxRows: int32(limit + 1)} // one extra row tells whether another page follows
	if p.Kind != nil {
		if !p.Kind.Valid() {
			return nil, &Error{Kind: KindInvalidRequest, Detail: "unknown kind"}
		}
		args.Kind = new(string(*p.Kind))
	}
	if p.Cursor != nil {
		var c healthLogCursor
		if err := s.cursors.Decode(*p.Cursor, &c); err != nil {
			return nil, err
		}
		args.AfterStart, args.AfterID = &c.Start, &c.ID
	}
	rows, err := s.q.ListHealthLog(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("listing health log: %w", err)
	}
	page := HealthLogPage{Items: []HealthLogEntry{}}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next, err := s.cursors.Encode(healthLogCursor{Start: last.StartDate, ID: last.ID})
		if err != nil {
			return nil, err
		}
		page.NextCursor = &next
	}
	for _, r := range rows {
		page.Items = append(page.Items, healthLogEntry(r))
	}
	return ListHealthLog200JSONResponse(page), nil
}

// CreateHealthLogEntry logs an injury, illness or period.
func (s *Server) CreateHealthLogEntry(ctx context.Context, req CreateHealthLogEntryRequestObject) (CreateHealthLogEntryResponseObject, error) {
	p, err := healthLogParams(req.Body)
	if err != nil {
		return nil, err
	}
	e, err := s.q.CreateHealthLogEntry(ctx, db.CreateHealthLogEntryParams{
		UserID: sessionFrom(ctx).userID, Kind: p.Kind, Description: p.Description, StartDate: p.StartDate, EndDate: p.EndDate,
	})
	if err != nil {
		return nil, fmt.Errorf("creating health log entry: %w", err)
	}
	return CreateHealthLogEntry201JSONResponse(healthLogEntry(e)), nil
}

// UpdateHealthLogEntry replaces a log entry.
func (s *Server) UpdateHealthLogEntry(ctx context.Context, req UpdateHealthLogEntryRequestObject) (UpdateHealthLogEntryResponseObject, error) {
	p, err := healthLogParams(req.Body)
	if err != nil {
		return nil, err
	}
	p.UserID, p.ID = sessionFrom(ctx).userID, req.EntryId
	e, err := s.q.UpdateHealthLogEntry(ctx, p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errHealthLogNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("updating health log entry: %w", err)
	}
	return UpdateHealthLogEntry200JSONResponse(healthLogEntry(e)), nil
}

// DeleteHealthLogEntry deletes a log entry.
func (s *Server) DeleteHealthLogEntry(ctx context.Context, req DeleteHealthLogEntryRequestObject) (DeleteHealthLogEntryResponseObject, error) {
	n, err := s.q.DeleteHealthLogEntry(ctx, db.DeleteHealthLogEntryParams{UserID: sessionFrom(ctx).userID, ID: req.EntryId})
	if err != nil {
		return nil, fmt.Errorf("deleting health log entry: %w", err)
	}
	if n == 0 {
		return nil, errHealthLogNotFound
	}
	return DeleteHealthLogEntry204Response{}, nil
}
