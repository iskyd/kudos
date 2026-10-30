package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/store/db"
)

// zoneCursor is the position after a page's last version.
type zoneCursor struct {
	From time.Time `json:"f"`
	ID   int64     `json:"i"`
}

// ListZoneSets returns one page of the user's zone versions, latest first.
func (s *Server) ListZoneSets(ctx context.Context, req ListZoneSetsRequestObject) (ListZoneSetsResponseObject, error) {
	p := req.Params
	limit := defaultPageSize
	if p.Limit != nil {
		limit = *p.Limit
	}
	if limit < 1 || limit > maxPageSize {
		return nil, &Error{Kind: KindInvalidRequest, Detail: fmt.Sprintf("limit must be 1 to %d", maxPageSize)}
	}
	args := db.ListZoneSetsParams{UserID: sessionFrom(ctx).userID, MaxRows: int32(limit + 1)} // one extra row tells whether another page follows
	if p.Sport != nil {
		if !p.Sport.Valid() {
			return nil, &Error{Kind: KindInvalidRequest, Detail: "unknown sport"}
		}
		args.Sport = new(string(*p.Sport))
	}
	if p.Cursor != nil {
		var c zoneCursor
		if err := s.cursors.Decode(*p.Cursor, &c); err != nil {
			return nil, err
		}
		args.AfterFrom, args.AfterID = &c.From, &c.ID
	}
	rows, err := s.q.ListZoneSets(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("listing zone versions: %w", err)
	}
	page := ZoneSetPage{Items: []ZoneSet{}}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next, err := s.cursors.Encode(zoneCursor{From: last.EffectiveFrom, ID: last.ID})
		if err != nil {
			return nil, err
		}
		page.NextCursor = &next
	}
	for _, r := range rows {
		page.Items = append(page.Items, zoneSet(r))
	}
	return ListZoneSets200JSONResponse(page), nil
}

// CreateZoneSet adds a version of a sport's thresholds.
func (s *Server) CreateZoneSet(ctx context.Context, req CreateZoneSetRequestObject) (CreateZoneSetResponseObject, error) {
	b := req.Body
	invalid := func(detail string) (CreateZoneSetResponseObject, error) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: detail}
	}
	switch {
	case !b.Sport.Valid():
		return invalid("unknown sport")
	case b.Sport == ZoneSportRunning && b.LthrBpm == nil && b.ThresholdSpeedMS == nil:
		return invalid("running needs lthrBpm or thresholdSpeedMS")
	case b.Sport == ZoneSportCycling && b.FtpW == nil && b.LthrBpm == nil:
		return invalid("cycling needs ftpW or lthrBpm")
	case b.Sport == ZoneSportSwimming && b.CssMS == nil:
		return invalid("swimming needs cssMS")
	case b.Sport == ZoneSportGeneral && (b.MaxHrBpm == nil || b.RestingHrBpm == nil):
		return invalid("general needs maxHrBpm and restingHrBpm")
	case (b.MaxHrBpm == nil) != (b.RestingHrBpm == nil):
		return invalid("maxHrBpm and restingHrBpm go together")
	case !inRange(b.LthrBpm, 60, 240) || !inRange(b.MaxHrBpm, 60, 250) || !inRange(b.RestingHrBpm, 20, 150) || !inRange(b.FtpW, 1, 2000):
		return invalid("a heart rate or power is out of range")
	case b.MaxHrBpm != nil && *b.RestingHrBpm >= *b.MaxHrBpm:
		return invalid("restingHrBpm must be below maxHrBpm")
	// No one's threshold is over 15 m/s, and the bound keeps CSS offsets meaningful.
	case !speedOK(b.ThresholdSpeedMS) || !speedOK(b.CssMS):
		return invalid("speeds must be above 0 and at most 15 m/s")
	}
	z, err := s.q.CreateZoneSet(ctx, db.CreateZoneSetParams{
		UserID: sessionFrom(ctx).userID, Sport: string(b.Sport), EffectiveFrom: b.EffectiveFrom.Time,
		LthrBpm: small(b.LthrBpm), ThresholdSpeedMS: f32(b.ThresholdSpeedMS), FtpW: small(b.FtpW),
		CssMS: f32(b.CssMS), MaxHrBpm: small(b.MaxHrBpm), RestingHrBpm: small(b.RestingHrBpm),
	})
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return invalid("a version of this sport already starts on that day")
	}
	if err != nil {
		return nil, fmt.Errorf("creating zone version: %w", err)
	}
	return CreateZoneSet201JSONResponse(zoneSet(z)), nil
}

// DeleteZoneSet deletes a version; its dates fall back to the one before.
func (s *Server) DeleteZoneSet(ctx context.Context, req DeleteZoneSetRequestObject) (DeleteZoneSetResponseObject, error) {
	n, err := s.q.DeleteZoneSet(ctx, db.DeleteZoneSetParams{UserID: sessionFrom(ctx).userID, ID: req.ZoneSetId})
	if err != nil {
		return nil, fmt.Errorf("deleting zone version: %w", err)
	}
	if n == 0 {
		return nil, &Error{Kind: KindNotFound, Detail: "no such zone version"}
	}
	return DeleteZoneSet204Response{}, nil
}

// GetActivityZones returns the zones valid on the activity's local date.
func (s *Server) GetActivityZones(ctx context.Context, req GetActivityZonesRequestObject) (GetActivityZonesResponseObject, error) {
	a, err := s.activity(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if a, err = ingest.Effective(a); err != nil {
		return nil, err
	}
	local := a.StartTime.Add(time.Duration(a.UtcOffsetS) * time.Second)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	rows, err := s.q.ListZoneSetsAt(ctx, db.ListZoneSetsAtParams{UserID: sessionFrom(ctx).userID, Day: day})
	if err != nil {
		return nil, fmt.Errorf("reading zone versions: %w", err)
	}
	sport := analytics.ZoneSportOf(a.Sport)
	out := ActivityZones{Sport: ZoneSport(sport), Day: openapi_types.Date{Time: day}}
	var own, general analytics.Thresholds
	for _, r := range rows {
		switch {
		case analytics.ZoneSport(r.Sport) == sport:
			own, out.ZoneSetId = thresholds(r), &r.ID
		case r.Sport == string(analytics.ZoneGeneral):
			general, out.GeneralZoneSetId = thresholds(r), &r.ID
		}
	}
	out.Zones = zonesOut(analytics.ZonesFor(sport, own, general))
	return GetActivityZones200JSONResponse(out), nil
}

func zoneSet(z db.ZoneSet) ZoneSet {
	return ZoneSet{
		Id: z.ID, Sport: ZoneSport(z.Sport), EffectiveFrom: openapi_types.Date{Time: z.EffectiveFrom},
		LthrBpm: intPtr(z.LthrBpm), ThresholdSpeedMS: f64(z.ThresholdSpeedMS), FtpW: intPtr(z.FtpW),
		CssMS: f64(z.CssMS), MaxHrBpm: intPtr(z.MaxHrBpm), RestingHrBpm: intPtr(z.RestingHrBpm),
		Zones: zonesOut(analytics.ZonesFor(analytics.ZoneSport(z.Sport), thresholds(z), analytics.Thresholds{})),
	}
}

func thresholds(z db.ZoneSet) analytics.Thresholds {
	return analytics.Thresholds{
		LTHR: i16f(z.LthrBpm), ThresholdSpeed: f64(z.ThresholdSpeedMS), FTP: i16f(z.FtpW),
		CSS: f64(z.CssMS), MaxHR: i16f(z.MaxHrBpm), RestingHR: i16f(z.RestingHrBpm),
	}
}

func zonesOut(z analytics.Zones) Zones {
	list := func(zs []analytics.Zone) *[]Zone {
		if zs == nil {
			return nil
		}
		out := make([]Zone, len(zs))
		for i, z := range zs {
			out[i] = Zone{Name: z.Name, Low: z.Low, High: z.High}
		}
		return &out
	}
	return Zones{
		HrModel: text(z.HRModel), Hr: list(z.HR), PaceModel: text(z.PaceModel), Pace: list(z.Pace),
		PowerModel: text(z.PowerModel), Power: list(z.Power),
	}
}

func inRange(v *int, lo, hi int) bool { return v == nil || (*v >= lo && *v <= hi) }

func speedOK(v *float64) bool { return v == nil || (*v > 0 && *v <= 15) }

func small(v *int) *int16 {
	if v == nil {
		return nil
	}
	return new(int16(*v)) //nolint:gosec // G115: validated to fit
}

func f32(v *float64) *float32 {
	if v == nil {
		return nil
	}
	return new(float32(*v))
}
