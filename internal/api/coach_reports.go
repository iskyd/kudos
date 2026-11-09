package api

import (
	"context"
	"fmt"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/iskyd/kudos/internal/store/db"
)

// ListCoachReports returns one page of the user's daily insights or weekly
// reports, latest day first.
func (s *Server) ListCoachReports(ctx context.Context, req ListCoachReportsRequestObject) (ListCoachReportsResponseObject, error) {
	p := req.Params
	if !p.Kind.Valid() {
		return nil, &Error{Kind: KindInvalidRequest, Detail: "kind must be daily_insight or weekly_report"}
	}
	limit := defaultPageSize
	if p.Limit != nil {
		limit = *p.Limit
	}
	if limit < 1 || limit > maxPageSize {
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("limit must be 1 to %d", maxPageSize)}
	}
	args := db.ListCoachReportsParams{
		UserID: sessionFrom(ctx).userID, Kind: string(p.Kind),
		MaxRows: int32(limit + 1), // one extra row tells whether another page follows
	}
	if p.Cursor != nil {
		var c wellnessCursor // a day, as for wellness days
		if err := s.cursors.Decode(*p.Cursor, &c); err != nil {
			return nil, err
		}
		args.BeforeDay = &c.Day
	}
	rows, err := s.q.ListCoachReports(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("listing coach reports: %w", err)
	}
	page := CoachReportPage{Items: []CoachReport{}}
	if len(rows) > limit {
		rows = rows[:limit]
		next, err := s.cursors.Encode(wellnessCursor{Day: rows[limit-1].Day})
		if err != nil {
			return nil, err
		}
		page.NextCursor = &next
	}
	for _, r := range rows {
		page.Items = append(page.Items, CoachReport{
			Id: r.ID, Kind: CoachReportKind(r.Kind), Day: openapi_types.Date{Time: r.Day},
			PromptVersion: r.PromptVersion, Content: r.Content, CreatedAt: r.CreatedAt,
		})
	}
	return ListCoachReports200JSONResponse(page), nil
}
