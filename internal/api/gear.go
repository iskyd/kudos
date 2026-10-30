package api

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/iskyd/kudos/internal/store/db"
)

// Keep in step with GearInput in api/openapi.yaml.
const maxGearNameLen = 80

var errGearNotFound = &Error{Kind: KindNotFound, Detail: "no such gear"}

// gearCursor is the position after a page's last gear.
type gearCursor struct {
	ID int64 `json:"i"`
}

// gearParams validates a create or update request and returns the stored values.
func gearParams(b *GearInput) (db.UpdateGearParams, error) {
	invalid := func(detail string) (db.UpdateGearParams, error) {
		return db.UpdateGearParams{}, &Error{Kind: KindInvalidRequest, Detail: detail}
	}
	if !b.Kind.Valid() {
		return invalid("kind must be shoes or bike")
	}
	name := strings.TrimSpace(b.Name)
	if name == "" || utf8.RuneCountInString(name) > maxGearNameLen {
		return invalid(fmt.Sprintf("name must be 1 to %d characters", maxGearNameLen))
	}
	if b.ReplacementDistanceM != nil && !(*b.ReplacementDistanceM > 0) {
		return invalid("replacementDistanceM must be positive")
	}
	return db.UpdateGearParams{
		Kind: string(b.Kind), Name: name, ReplacementDistanceM: b.ReplacementDistanceM, Retired: b.Retired != nil && *b.Retired,
	}, nil
}

func gear(r db.ListGearTotalsRow) Gear {
	g := r.Gear
	return Gear{
		Id: g.ID, Kind: GearKind(g.Kind), Name: g.Name, ReplacementDistanceM: g.ReplacementDistanceM, Retired: g.Retired,
		DistanceM: r.DistanceM, TimeS: r.TimeS, Activities: r.Activities,
		ReplacementDue: g.ReplacementDistanceM != nil && r.DistanceM >= *g.ReplacementDistanceM,
	}
}

// oneGear returns the user's gear with its totals, or not-found for any other.
func (s *Server) oneGear(ctx context.Context, id int64) (Gear, error) {
	rows, err := s.q.ListGearTotals(ctx, db.ListGearTotalsParams{UserID: sessionFrom(ctx).userID, ID: &id, MaxRows: 1})
	if err != nil {
		return Gear{}, fmt.Errorf("reading gear: %w", err)
	}
	if len(rows) == 0 {
		return Gear{}, errGearNotFound
	}
	return gear(rows[0]), nil
}

// ListGear returns one page of the user's gear with distance and time totals.
func (s *Server) ListGear(ctx context.Context, req ListGearRequestObject) (ListGearResponseObject, error) {
	limit := defaultPageSize
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if limit < 1 || limit > maxPageSize {
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("limit must be 1 to %d", maxPageSize)}
	}
	var after gearCursor
	if req.Params.Cursor != nil {
		if err := s.cursors.Decode(*req.Params.Cursor, &after); err != nil {
			return nil, err
		}
	}
	rows, err := s.q.ListGearTotals(ctx, db.ListGearTotalsParams{
		UserID: sessionFrom(ctx).userID, After: after.ID,
		MaxRows: int32(limit + 1), // one extra row tells whether another page follows
	})
	if err != nil {
		return nil, fmt.Errorf("listing gear: %w", err)
	}
	page := GearPage{Items: []Gear{}}
	if len(rows) > limit {
		rows = rows[:limit]
		next, err := s.cursors.Encode(gearCursor{ID: rows[limit-1].Gear.ID})
		if err != nil {
			return nil, err
		}
		page.NextCursor = &next
	}
	for _, r := range rows {
		page.Items = append(page.Items, gear(r))
	}
	return ListGear200JSONResponse(page), nil
}

// CreateGear adds shoes or a bike.
func (s *Server) CreateGear(ctx context.Context, req CreateGearRequestObject) (CreateGearResponseObject, error) {
	p, err := gearParams(req.Body)
	if err != nil {
		return nil, err
	}
	g, err := s.q.CreateGear(ctx, db.CreateGearParams{
		UserID: sessionFrom(ctx).userID, Kind: p.Kind, Name: p.Name, ReplacementDistanceM: p.ReplacementDistanceM, Retired: p.Retired,
	})
	if err != nil {
		return nil, fmt.Errorf("creating gear: %w", err)
	}
	return CreateGear201JSONResponse(gear(db.ListGearTotalsRow{Gear: g})), nil
}

// UpdateGear replaces the gear's name, kind, threshold and retirement.
func (s *Server) UpdateGear(ctx context.Context, req UpdateGearRequestObject) (UpdateGearResponseObject, error) {
	p, err := gearParams(req.Body)
	if err != nil {
		return nil, err
	}
	p.UserID, p.ID = sessionFrom(ctx).userID, req.Id
	n, err := s.q.UpdateGear(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("updating gear: %w", err)
	}
	if n == 0 {
		return nil, errGearNotFound
	}
	g, err := s.oneGear(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return UpdateGear200JSONResponse(g), nil
}

// DeleteGear deletes gear; its activities keep everything but the gear.
func (s *Server) DeleteGear(ctx context.Context, req DeleteGearRequestObject) (DeleteGearResponseObject, error) {
	n, err := s.q.DeleteGear(ctx, db.DeleteGearParams{UserID: sessionFrom(ctx).userID, ID: req.Id})
	if err != nil {
		return nil, fmt.Errorf("deleting gear: %w", err)
	}
	if n == 0 {
		return nil, errGearNotFound
	}
	return DeleteGear204Response{}, nil
}
