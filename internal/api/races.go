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

// Keep in step with RaceInput in api/openapi.yaml.
const maxRaceName = 200

var errRaceNotFound = &Error{Kind: KindNotFound, Detail: "no such race"}

// raceCursor is the position after a page's last race.
type raceCursor struct {
	Day time.Time `json:"d"`
	ID  int64     `json:"i"`
}

func race(r db.Race) Race {
	return Race{
		Id: r.ID, Name: r.Name, Day: openapi_types.Date{Time: r.Day}, Sport: ActivitySport(r.Sport),
		DistanceM: r.DistanceM, GoalTimeS: r.GoalTimeS, Priority: RacePriority(r.Priority),
	}
}

// raceParams validates a race and returns its stored values.
func raceParams(b *RaceInput) (db.CreateRaceParams, error) {
	invalid := func(detail string) (db.CreateRaceParams, error) {
		return db.CreateRaceParams{}, &Error{Kind: KindInvalidRequest, Detail: detail}
	}
	name := strings.TrimSpace(b.Name)
	switch {
	case name == "" || utf8.RuneCountInString(name) > maxRaceName:
		return invalid(fmt.Sprintf("name must be 1 to %d characters", maxRaceName))
	case !b.Sport.Valid():
		return invalid("unknown sport")
	case !(b.DistanceM > 0):
		return invalid("distanceM must be positive")
	case b.GoalTimeS != nil && !(*b.GoalTimeS > 0):
		return invalid("goalTimeS must be positive")
	case !b.Priority.Valid():
		return invalid("priority must be A, B or C")
	}
	return db.CreateRaceParams{
		Name: name, Day: b.Day.Time, Sport: string(b.Sport), DistanceM: b.DistanceM, GoalTimeS: b.GoalTimeS, Priority: string(b.Priority),
	}, nil
}

// ListRaces returns one page of the user's races, latest race day first.
func (s *Server) ListRaces(ctx context.Context, req ListRacesRequestObject) (ListRacesResponseObject, error) {
	limit := defaultPageSize
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if limit < 1 || limit > maxPageSize {
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("limit must be 1 to %d", maxPageSize)}
	}
	args := db.ListRacesParams{UserID: sessionFrom(ctx).userID, MaxRows: int32(limit + 1)} // one extra row tells whether another page follows
	if req.Params.Cursor != nil {
		var c raceCursor
		if err := s.cursors.Decode(*req.Params.Cursor, &c); err != nil {
			return nil, err
		}
		args.AfterDay, args.AfterID = &c.Day, &c.ID
	}
	rows, err := s.q.ListRaces(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("listing races: %w", err)
	}
	page := RacePage{Items: []Race{}}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next, err := s.cursors.Encode(raceCursor{Day: last.Day, ID: last.ID})
		if err != nil {
			return nil, err
		}
		page.NextCursor = &next
	}
	for _, r := range rows {
		page.Items = append(page.Items, race(r))
	}
	return ListRaces200JSONResponse(page), nil
}

// CreateRace adds a race.
func (s *Server) CreateRace(ctx context.Context, req CreateRaceRequestObject) (CreateRaceResponseObject, error) {
	p, err := raceParams(req.Body)
	if err != nil {
		return nil, err
	}
	p.UserID = sessionFrom(ctx).userID
	r, err := s.q.CreateRace(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("creating race: %w", err)
	}
	return CreateRace201JSONResponse(race(r)), nil
}

// UpdateRace replaces a race.
func (s *Server) UpdateRace(ctx context.Context, req UpdateRaceRequestObject) (UpdateRaceResponseObject, error) {
	p, err := raceParams(req.Body)
	if err != nil {
		return nil, err
	}
	r, err := s.q.UpdateRace(ctx, db.UpdateRaceParams{
		UserID: sessionFrom(ctx).userID, ID: req.Id, Name: p.Name, Day: p.Day, Sport: p.Sport,
		DistanceM: p.DistanceM, GoalTimeS: p.GoalTimeS, Priority: p.Priority,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errRaceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("updating race: %w", err)
	}
	return UpdateRace200JSONResponse(race(r)), nil
}

// DeleteRace deletes a race.
func (s *Server) DeleteRace(ctx context.Context, req DeleteRaceRequestObject) (DeleteRaceResponseObject, error) {
	n, err := s.q.DeleteRace(ctx, db.DeleteRaceParams{UserID: sessionFrom(ctx).userID, ID: req.Id})
	if err != nil {
		return nil, fmt.Errorf("deleting race: %w", err)
	}
	if n == 0 {
		return nil, errRaceNotFound
	}
	return DeleteRace204Response{}, nil
}
