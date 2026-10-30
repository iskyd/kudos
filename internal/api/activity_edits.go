package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/store/db"
)

// AddActivityEdit stores one edit; offsets are from the activity's current start.
func (s *Server) AddActivityEdit(ctx context.Context, req AddActivityEditRequestObject) (AddActivityEditResponseObject, error) {
	b := req.Body
	invalid := func(detail string) (AddActivityEditResponseObject, error) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: detail}
	}
	a, err := s.activity(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	eff, err := ingest.Effective(a)
	if err != nil {
		return nil, err
	}
	at := func(offset *float64) *time.Time {
		return new(eff.StartTime.Add(time.Duration(*offset * float64(time.Second))))
	}
	e := ingest.Edit{Kind: string(b.Kind)}
	switch b.Kind {
	case ActivityEditKindSport:
		if b.Sport == nil || !b.Sport.Valid() || b.Subtype == nil || !b.Subtype.Valid() {
			return invalid("a sport change needs a known sport and subtype")
		}
		e.Sport, e.Subtype = string(*b.Sport), string(*b.Subtype)
	case ActivityEditKindTrim, ActivityEditKindExcludeHr:
		if b.FromS == nil || b.ToS == nil || *b.FromS < 0 || *b.ToS <= *b.FromS {
			return invalid("fromS and toS must be offsets with toS after fromS")
		}
		e.From, e.To = at(b.FromS), at(b.ToS)
	case ActivityEditKindSplit:
		if b.AtS == nil || *b.AtS <= 0 {
			return invalid("atS must be a positive offset")
		}
		e.At = at(b.AtS)
	case ActivityEditKindMerge:
		if b.ActivityId == nil {
			return invalid("a merge needs activityId")
		}
		e.ActivityID = *b.ActivityId
	default:
		return invalid("unknown edit kind")
	}

	err = s.ingest.AddEdit(ctx, sessionFrom(ctx).userID, req.Id, e)
	var bad ingest.InvalidEditError
	switch {
	case errors.Is(err, ingest.ErrNotFound):
		return nil, errActivityNotFound
	case errors.As(err, &bad):
		return invalid(string(bad))
	case err != nil:
		return nil, fmt.Errorf("editing: %w", err)
	}
	out, err := s.reread(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return AddActivityEdit200JSONResponse(out), nil
}

// UndoActivityEdit removes one edit.
func (s *Server) UndoActivityEdit(ctx context.Context, req UndoActivityEditRequestObject) (UndoActivityEditResponseObject, error) {
	if _, err := s.activity(ctx, req.Id); err != nil {
		return nil, err
	}
	err := s.ingest.UndoEdit(ctx, sessionFrom(ctx).userID, req.Id, req.EditId)
	if errors.Is(err, ingest.ErrEditNotFound) {
		return nil, &Error{Kind: KindNotFound, Detail: "no such edit"}
	}
	if err != nil {
		return nil, fmt.Errorf("undoing: %w", err)
	}
	out, err := s.reread(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return UndoActivityEdit200JSONResponse(out), nil
}

// reread returns the activity's detail after a change.
func (s *Server) reread(ctx context.Context, id int64) (ActivityDetail, error) {
	a, err := s.activity(ctx, id)
	if err != nil {
		return ActivityDetail{}, err
	}
	return s.detail(ctx, a)
}

// activityEdits lists an activity's edits for its detail.
func (s *Server) activityEdits(ctx context.Context, id int64) ([]ActivityEdit, error) {
	rows, err := s.q.ListActivityEdits(ctx, db.ListActivityEditsParams{UserID: sessionFrom(ctx).userID, ActivityID: id})
	if err != nil {
		return nil, fmt.Errorf("listing edits: %w", err)
	}
	out := make([]ActivityEdit, len(rows))
	for i, r := range rows {
		e, err := ingest.DecodeEdit(r)
		if err != nil {
			return nil, err
		}
		out[i] = ActivityEdit{Id: r.ID, Kind: ActivityEditKind(r.Kind), CreatedAt: r.CreatedAt, From: e.From, To: e.To, At: e.At}
		if e.Sport != "" {
			out[i].Sport, out[i].Subtype = new(ActivitySport(e.Sport)), new(ActivityEditSubtype(e.Subtype))
		}
		if e.ActivityID != 0 {
			out[i].ActivityId = &e.ActivityID
		}
	}
	return out, nil
}
