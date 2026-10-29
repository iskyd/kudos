package garmin

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"time"

	"github.com/iskyd/kudos/internal/connectors"
)

const (
	pageSize = 100
	// maxPages bounds a search; a one-year window never comes near it.
	maxPages     = 100
	garminLayout = time.DateTime // "2026-10-28 11:56:31"
)

type searchedActivity struct {
	ActivityID     int64  `json:"activityId"`
	StartTimeGMT   string `json:"startTimeGMT"`
	StartTimeLocal string `json:"startTimeLocal"`
}

// FetchActivities lists the activities that started on the local days from..to,
// oldest first.
func (c *Client) FetchActivities(ctx context.Context, s *connectors.Session, from, to time.Time) ([]connectors.RawActivity, error) {
	var out []connectors.RawActivity
	for page := range maxPages {
		q := url.Values{
			"startDate": {from.Format(time.DateOnly)}, "endDate": {to.Format(time.DateOnly)},
			"start": {strconv.Itoa(page * pageSize)}, "limit": {strconv.Itoa(pageSize)}, "sortOrder": {"asc"},
		}
		var found []searchedActivity
		if err := c.getJSON(ctx, s, "/activitylist-service/activities/search/activities", q, &found); err != nil {
			return nil, err
		}
		for _, a := range found {
			raw, err := rawActivity(a)
			if err != nil {
				return nil, err
			}
			out = append(out, raw)
		}
		if len(found) < pageSize {
			return out, nil
		}
	}
	return nil, fmt.Errorf("%w: more than %d activities in the range", connectors.ErrUpstream, maxPages*pageSize)
}

func rawActivity(a searchedActivity) (connectors.RawActivity, error) {
	start, err := time.Parse(garminLayout, a.StartTimeGMT)
	if a.ActivityID <= 0 || err != nil {
		return connectors.RawActivity{}, fmt.Errorf("%w: activity %d has no valid ID or start time", connectors.ErrUpstream, a.ActivityID)
	}
	raw := connectors.RawActivity{SourceID: strconv.FormatInt(a.ActivityID, 10), Start: start}
	if local, err := time.Parse(garminLayout, a.StartTimeLocal); err == nil {
		offset := local.Sub(start)
		raw.LocalOffset = &offset
	}
	return raw, nil
}

// DownloadActivityFile returns the original file of an activity. Garmin sends a
// ZIP holding that one file.
func (c *Client) DownloadActivityFile(ctx context.Context, s *connectors.Session, sourceID string) (connectors.File, error) {
	body, err := c.get(ctx, s, "/download-service/files/activity/"+url.PathEscape(sourceID), nil)
	if err != nil {
		return connectors.File{}, err
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return connectors.File{}, fmt.Errorf("%w: activity %s download is not a ZIP: %w", connectors.ErrUpstream, sourceID, err)
	}
	if len(zr.File) != 1 {
		return connectors.File{}, fmt.Errorf("%w: activity %s download holds %d files, want 1", connectors.ErrUpstream, sourceID, len(zr.File))
	}
	f, err := zr.File[0].Open()
	if err != nil {
		return connectors.File{}, fmt.Errorf("%w: opening activity %s download: %w", connectors.ErrUpstream, sourceID, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxBody+1))
	if err != nil {
		return connectors.File{}, fmt.Errorf("%w: reading activity %s download: %w", connectors.ErrUpstream, sourceID, err)
	}
	if len(data) > maxBody {
		return connectors.File{}, fmt.Errorf("%w: activity %s file over %d bytes", connectors.ErrUpstream, sourceID, maxBody)
	}
	return connectors.File{Name: zr.File[0].Name, Data: data}, nil
}
