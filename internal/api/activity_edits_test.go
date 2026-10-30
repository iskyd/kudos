package api

import (
	"fmt"
	"net/http"
	"testing"
)

func editActivity(t *testing.T, c *apiClient, id int64, body ActivityEditRequest) ActivityDetail {
	t.Helper()
	return decodeJSON[ActivityDetail](t, c.do(http.MethodPost, fmt.Sprintf("/api/activities/%d/edits", id), body, nil), http.StatusOK)
}

func undo(t *testing.T, c *apiClient, id, editID int64) ActivityDetail {
	t.Helper()
	return decodeJSON[ActivityDetail](t, c.do(http.MethodDelete, fmt.Sprintf("/api/activities/%d/edits/%d", id, editID), nil, nil), http.StatusOK)
}

func streams(t *testing.T, c *apiClient, id int64) ActivityStreams {
	t.Helper()
	return decodeJSON[ActivityStreams](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d/streams?maxPoints=10000", id), nil, nil), http.StatusOK)
}

func TestTrimAndExcludeHeartRate(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	id := uploadCorpus(t, c, "intervals.fit")
	recorded := decodeJSON[ActivityDetail](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d", id), nil, nil), http.StatusOK)
	full := streams(t, c, id)

	a := editActivity(t, c, id, ActivityEditRequest{Kind: ActivityEditKindTrim, FromS: new(600.0), ToS: new(1200.0)})
	if len(a.Edits) != 1 || a.Edits[0].Kind != ActivityEditKindTrim || !a.StartTime.After(recorded.StartTime) ||
		*a.ElapsedTimeS > 600 || *a.DistanceM >= *recorded.DistanceM || a.CaloriesKcal != nil {
		t.Errorf("after trimming: %+v", a)
	}
	if from := a.Edits[0].From; from == nil || from.Sub(recorded.StartTime).Seconds() != 600 {
		t.Errorf("trim starts at %v", from)
	}
	trimmed := streams(t, c, id)
	if trimmed.TimeS[0] != 0 || trimmed.TimeS[len(trimmed.TimeS)-1] > 600 || len(trimmed.TimeS) >= len(full.TimeS) {
		t.Errorf("trimmed streams: %d samples ending at %v", len(trimmed.TimeS), trimmed.TimeS[len(trimmed.TimeS)-1])
	}

	// Offsets now count from the trimmed start.
	a = editActivity(t, c, id, ActivityEditRequest{Kind: ActivityEditKindExcludeHr, FromS: new(0.0), ToS: new(600.0)})
	if a.AvgHrBpm != nil || streams(t, c, id).HrBpm != nil {
		t.Errorf("heart rate kept: %v", a.AvgHrBpm)
	}

	a = editActivity(t, c, id, ActivityEditRequest{Kind: ActivityEditKindSport, Sport: new(ActivitySportTrailRunning), Subtype: new(ActivityEditRequestSubtypeOutdoor)})
	if a.Sport != ActivitySportTrailRunning || *a.Edits[2].Sport != ActivitySportTrailRunning {
		t.Errorf("sport = %s", a.Sport)
	}
	page := decodeJSON[ActivityPage](t, c.do(http.MethodGet, "/api/activities?sport=trail_running", nil, nil), http.StatusOK)
	if len(page.Items) != 1 || *page.Items[0].ElapsedTimeS > 600 {
		t.Errorf("list after editing: %+v", page.Items)
	}

	for _, e := range a.Edits {
		a = undo(t, c, id, e.Id)
	}
	if len(a.Edits) != 0 || !a.StartTime.Equal(recorded.StartTime) || *a.DistanceM != *recorded.DistanceM ||
		*a.AvgHrBpm != *recorded.AvgHrBpm || a.Sport != recorded.Sport {
		t.Errorf("after undoing: %+v", a)
	}
	if got := streams(t, c, id); len(got.TimeS) != len(full.TimeS) {
		t.Errorf("%d samples after undoing, want %d", len(got.TimeS), len(full.TimeS))
	}
	wantProblem(t, c.do(http.MethodDelete, fmt.Sprintf("/api/activities/%d/edits/%d", id, a.Id), nil, nil), KindNotFound)
}

func TestStreamsPageThroughAnEditedView(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	id := uploadCorpus(t, c, "intervals.fit")
	editActivity(t, c, id, ActivityEditRequest{Kind: ActivityEditKindTrim, FromS: new(60.0), ToS: new(2000.0)})
	want := len(streams(t, c, id).TimeS)
	n := pageAll(t, c, fmt.Sprintf("/api/activities/%d/streams", id),
		func(p ActivityStreams) int { return len(p.TimeS) }, func(p ActivityStreams) *string { return p.NextCursor })
	if n != want {
		t.Errorf("paged %d samples, want %d", n, want)
	}
}

func TestSplitAndMerge(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	run := uploadCorpus(t, c, "intervals.fit")
	half := uploadCorpus(t, c, "half_marathon.fit")
	list := func() []ActivitySummary {
		return decodeJSON[ActivityPage](t, c.do(http.MethodGet, "/api/activities", nil, nil), http.StatusOK).Items
	}

	a := editActivity(t, c, run, ActivityEditRequest{Kind: ActivityEditKindSplit, AtS: new(1800.0)})
	second := *a.Edits[0].ActivityId
	if len(list()) != 3 {
		t.Errorf("listed after splitting: %d", len(list()))
	}
	part := decodeJSON[ActivityDetail](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d", second), nil, nil), http.StatusOK)
	if part.Source != ActivityDetailSourceSplit || *part.SplitOf != run || part.DistanceM == nil {
		t.Errorf("second part: %+v", part)
	}
	if len(streams(t, c, second).TimeS) == 0 {
		t.Error("the second part has no samples")
	}
	a = undo(t, c, run, a.Edits[0].Id)
	wantProblem(t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d", second), nil, nil), KindNotFound)

	a = editActivity(t, c, run, ActivityEditRequest{Kind: ActivityEditKindMerge, ActivityId: &half})
	if items := list(); len(items) != 1 || items[0].Id != run {
		t.Errorf("listed after merging: %+v", items)
	}
	merged := decodeJSON[ActivityDetail](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d", half), nil, nil), http.StatusOK)
	if merged.MergedInto == nil || *merged.MergedInto != run {
		t.Errorf("merged into %v", merged.MergedInto)
	}
	wantProblem(t, c.do(http.MethodPost, fmt.Sprintf("/api/activities/%d/edits", half),
		ActivityEditRequest{Kind: ActivityEditKindSplit, AtS: new(60.0)}, nil), KindInvalidRequest)
	undo(t, c, run, a.Edits[0].Id)
	if len(list()) != 2 {
		t.Errorf("listed after undoing the merge: %d", len(list()))
	}
}

func TestActivityEditValidation(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	id := uploadCorpus(t, c, "intervals.fit")
	path := fmt.Sprintf("/api/activities/%d/edits", id)
	for name, body := range map[string]ActivityEditRequest{
		"kind":          {Kind: "rotate"},
		"sport":         {Kind: ActivityEditKindSport, Sport: new(ActivitySport("curling")), Subtype: new(ActivityEditRequestSubtypeOutdoor)},
		"no subtype":    {Kind: ActivityEditKindSport, Sport: new(ActivitySportRunning)},
		"no range":      {Kind: ActivityEditKindTrim, FromS: new(10.0)},
		"empty range":   {Kind: ActivityEditKindExcludeHr, FromS: new(10.0), ToS: new(10.0)},
		"no split":      {Kind: ActivityEditKindSplit},
		"split at 0":    {Kind: ActivityEditKindSplit, AtS: new(0.0)},
		"no merge":      {Kind: ActivityEditKindMerge},
		"merge itself":  {Kind: ActivityEditKindMerge, ActivityId: &id},
		"trim past end": {Kind: ActivityEditKindTrim, FromS: new(1e6), ToS: new(2e6)},
	} {
		t.Run(name, func(t *testing.T) {
			c.t = t
			wantProblem(t, c.do(http.MethodPost, path, body, nil), KindInvalidRequest)
		})
	}
	c.t = t

	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	sport := ActivityEditRequest{Kind: ActivityEditKindSport, Sport: new(ActivitySportCycling), Subtype: new(ActivityEditRequestSubtypeIndoor)}
	wantProblem(t, bob.do(http.MethodPost, path, sport, nil), KindNotFound)
	a := editActivity(t, c, id, sport)
	wantProblem(t, bob.do(http.MethodDelete, fmt.Sprintf("%s/%d", path, a.Edits[0].Id), nil, nil), KindNotFound)
	if got := decodeJSON[ActivityDetail](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d", id), nil, nil), http.StatusOK); len(got.Edits) != 1 {
		t.Errorf("alice's edits: %d", len(got.Edits))
	}
}
