package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/analytics"
	"github.com/iskyd/kudos/internal/importer"
	"github.com/iskyd/kudos/internal/store/db"
)

// thresholdValues are the thresholds a suggestion compares.
type thresholdValues struct{ lthr, ftp, maxHR, restingHR *int16 }

// coveredBy reports whether known already holds every value v has.
func (v thresholdValues) coveredBy(known thresholdValues) bool {
	same := func(a, b *int16) bool { return a == nil || (b != nil && *a == *b) }
	return same(v.lthr, known.lthr) && same(v.ftp, known.ftp) && same(v.maxHR, known.maxHR) && same(v.restingHR, known.restingHR)
}

func (v thresholdValues) equal(o thresholdValues) bool { return v.coveredBy(o) && o.coveredBy(v) }

// suggestZones records the file's thresholds as a zone suggestion for the
// activity's local date when they differ from what is known for its sport then:
// the latest zone version or suggestion (pending or dismissed) on or before it.
// Runs suggest running versions, rides cycling ones, everything else general
// ones (max and resting HR). A watch's FTP on a run is running power and is
// never used. The first file of a day wins, and a version set for that day
// stands. An older file with the same values as the next suggestion moves that
// suggestion back to its date. a.Thresholds must be set.
func suggestZones(ctx context.Context, q *db.Queries, userID, activityID int64, a importer.Activity, offsetS int32) error {
	th := a.Thresholds
	obs := thresholdValues{maxHR: i16(th.MaxHR), restingHR: i16(th.RestingHR)}
	sport := analytics.ZoneSportOf(string(a.Sport))
	switch sport {
	case analytics.ZoneRunning:
		obs.lthr = i16(th.ThresholdHR)
	case analytics.ZoneCycling:
		obs.lthr, obs.ftp = i16(th.ThresholdHR), i16(th.FTP)
	default:
		sport = analytics.ZoneGeneral
	}
	if obs.equal(thresholdValues{}) {
		return nil
	}
	local := a.StartTime.Add(time.Duration(offsetS) * time.Second)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)

	known, from, err := knownOn(ctx, q, userID, string(sport), day)
	if err != nil {
		return err
	}
	if from != nil && (from.Equal(day) || obs.coveredBy(known)) {
		return nil
	}
	next, err := q.GetNextZoneSuggestion(ctx, db.GetNextZoneSuggestionParams{UserID: userID, Sport: string(sport), Day: day})
	switch {
	case err == nil && obs.equal(suggestionValues(next)):
		err = q.MoveZoneSuggestion(ctx, db.MoveZoneSuggestionParams{UserID: userID, ID: next.ID, EffectiveFrom: day, ActivityID: &activityID})
	case err == nil || errors.Is(err, pgx.ErrNoRows):
		err = q.InsertZoneSuggestion(ctx, db.InsertZoneSuggestionParams{
			UserID: userID, Sport: string(sport), EffectiveFrom: day, ActivityID: &activityID,
			LthrBpm: obs.lthr, FtpW: obs.ftp, MaxHrBpm: obs.maxHR, RestingHrBpm: obs.restingHR,
		})
	}
	if err != nil {
		return fmt.Errorf("suggesting zones: %w", err)
	}
	return nil
}

// knownOn returns the thresholds known for a sport on day and the date they
// start, from whichever of the version and the suggestion starts later; from is
// nil when there are none.
func knownOn(ctx context.Context, q *db.Queries, userID int64, sport string, day time.Time) (thresholdValues, *time.Time, error) {
	var known thresholdValues
	var from *time.Time
	v, err := q.GetZoneSetAt(ctx, db.GetZoneSetAtParams{UserID: userID, Sport: sport, Day: day})
	switch {
	case err == nil:
		known, from = thresholdValues{v.LthrBpm, v.FtpW, v.MaxHrBpm, v.RestingHrBpm}, &v.EffectiveFrom
	case !errors.Is(err, pgx.ErrNoRows):
		return known, nil, fmt.Errorf("reading zone version: %w", err)
	}
	s, err := q.GetZoneSuggestionAt(ctx, db.GetZoneSuggestionAtParams{UserID: userID, Sport: sport, Day: day})
	switch {
	case err == nil && (from == nil || s.EffectiveFrom.After(*from)):
		known, from = suggestionValues(s), &s.EffectiveFrom
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return known, nil, fmt.Errorf("reading zone suggestion: %w", err)
	}
	return known, from, nil
}

func suggestionValues(s db.ZoneSuggestion) thresholdValues {
	return thresholdValues{s.LthrBpm, s.FtpW, s.MaxHrBpm, s.RestingHrBpm}
}
