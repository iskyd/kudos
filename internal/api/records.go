package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/store/db"
)

// maxRecordRows bounds the record history read at once.
// shortcut: history past 5000 record-setting activities is cut; page per effort if anyone gets close.
const maxRecordRows = 5000

// GetRecords returns every record that has been set with its history.
func (s *Server) GetRecords(ctx context.Context, _ GetRecordsRequestObject) (GetRecordsResponseObject, error) {
	userID := sessionFrom(ctx).userID
	rows, err := s.q.ListRecordHistory(ctx, db.ListRecordHistoryParams{UserID: userID, MaxRows: maxRecordRows})
	if err != nil {
		return nil, fmt.Errorf("listing records: %w", err)
	}
	history := map[string][]RecordEntry{}
	for _, r := range rows {
		history[r.Effort] = append(history[r.Effort], RecordEntry{
			ActivityId: r.ActivityID, StartTime: r.StartTime, UtcOffsetS: int(r.UtcOffsetS), Value: r.Value,
		})
	}
	out := GetRecords200JSONResponse{Records: []Record{}}
	for _, e := range analytics.Efforts() {
		if h := history[string(e)]; h != nil {
			out.Records = append(out.Records, Record{Effort: RecordEffort(e), History: h})
		}
	}
	switch since, err := s.q.GetRecordsSince(ctx, userID); {
	case err == nil:
		out.Since = &openapi_types.Date{Time: since}
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("getting records start: %w", err)
	}
	return out, nil
}
