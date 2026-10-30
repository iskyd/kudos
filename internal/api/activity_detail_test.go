package api

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iskyd/kudos/internal/store/db"
)

// uploadCorpus uploads a FIT file from the corpus and returns its activity ID.
func uploadCorpus(t *testing.T, c *apiClient, name string) int64 {
	t.Helper()
	data, err := os.ReadFile("../../testdata/fit/" + name)
	if err != nil {
		t.Fatal(err)
	}
	res := decodeJSON[UploadResult](t, c.upload("file", name, data), http.StatusCreated)
	return res.ActivityIds[0]
}

// pageAll follows nextCursor through a lap, length or set list two items at a time.
func pageAll[P any](t *testing.T, c *apiClient, path string, items func(P) int, next func(P) *string) int {
	t.Helper()
	total, url := 0, path+"?limit=2"
	for range 1000 {
		p := decodeJSON[P](t, c.do(http.MethodGet, url, nil, nil), http.StatusOK)
		total += items(p)
		if next(p) == nil {
			return total
		}
		url = path + "?limit=2&cursor=" + *next(p)
	}
	t.Fatal("too many pages")
	return 0
}

func TestGetActivityAndLaps(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "UTC")
	id := uploadCorpus(t, c, "intervals.fit")
	path := fmt.Sprintf("/api/activities/%d", id)

	a := decodeJSON[ActivityDetail](t, c.do(http.MethodGet, path, nil, nil), http.StatusOK)
	if a.Id != id || a.Sport != ActivitySportRunning || a.Workout == nil || len(a.Workout.Steps) == 0 || len(a.Legs) != 0 ||
		a.MaxHrBpm == nil || a.DistanceM == nil {
		t.Fatalf("detail = %+v", a)
	}
	step := a.Workout.Steps[0]
	if step.Index != 0 || step.DurationType == "" {
		t.Errorf("first step = %+v", step)
	}

	stored, err := q.ListLaps(t.Context(), db.ListLapsParams{UserID: me.Id, ActivityID: id, After: -1, MaxRows: 1000})
	if err != nil || len(stored) < 3 {
		t.Fatalf("stored laps = %d, %v", len(stored), err)
	}
	n := pageAll(t, c, path+"/laps", func(p LapPage) int { return len(p.Items) }, func(p LapPage) *string { return p.NextCursor })
	if n != len(stored) {
		t.Errorf("paged %d laps, want %d", n, len(stored))
	}
	first := decodeJSON[LapPage](t, c.do(http.MethodGet, path+"/laps?limit=1", nil, nil), http.StatusOK)
	if l := first.Items[0]; l.Index != 0 || l.WorkoutStepIndex == nil || l.Intensity == nil || l.DistanceM == nil {
		t.Errorf("first lap = %+v", l)
	}
	wantProblem(t, c.do(http.MethodGet, path+"/laps?cursor=bogus", nil, nil), KindInvalidRequest)
	wantProblem(t, c.do(http.MethodGet, path+"/laps?limit=101", nil, nil), KindInvalidRequest)
	// A strength-free activity has no sets: an empty page.
	if p := decodeJSON[StrengthSetPage](t, c.do(http.MethodGet, path+"/sets", nil, nil), http.StatusOK); len(p.Items) != 0 || p.NextCursor != nil {
		t.Errorf("sets = %+v", p)
	}
}

func TestActivityDetailIsolation(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	id := uploadCorpus(t, c, "pool_swim.fit")
	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)

	for _, sub := range []string{"", "/laps", "/lengths", "/sets", "/streams", "/streams?maxPoints=10"} {
		wantProblem(t, bob.do(http.MethodGet, fmt.Sprintf("/api/activities/%d%s", id, sub), nil, nil), KindNotFound)
		wantProblem(t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d%s", math.MaxInt64, sub), nil, nil), KindNotFound)
	}
}

func TestListSwimLengths(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	id := uploadCorpus(t, c, "pool_swim.fit")
	path := fmt.Sprintf("/api/activities/%d/lengths", id)
	// A pool swim records no position: its streams have no lat or lon column.
	if s := decodeJSON[ActivityStreams](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d/streams", id), nil, nil), http.StatusOK); s.Lat != nil || s.Lon != nil || s.HrBpm == nil {
		t.Errorf("pool swim columns: lat %v, hr %v", s.Lat != nil, s.HrBpm != nil)
	}

	var active, rest int
	url := path + "?limit=100"
	for {
		p := decodeJSON[SwimLengthPage](t, c.do(http.MethodGet, url, nil, nil), http.StatusOK)
		for _, l := range p.Items {
			switch {
			case l.Active && (l.Swolf == nil || l.Strokes == nil || l.TimerTimeS == nil || *l.Swolf != *l.TimerTimeS+float64(*l.Strokes)):
				t.Errorf("active length %d: swolf %v, strokes %v, time %v", l.Index, l.Swolf, l.Strokes, l.TimerTimeS)
			case l.Active:
				active++
			case l.Swolf != nil:
				t.Errorf("rest length %d has swolf %v", l.Index, *l.Swolf)
			default:
				rest++
			}
		}
		if p.NextCursor == nil {
			break
		}
		url = path + "?limit=100&cursor=" + *p.NextCursor
	}
	if active == 0 || rest == 0 {
		t.Errorf("%d active and %d rest lengths, want both", active, rest)
	}
}

func TestListStrengthSets(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "UTC")
	id := uploadCorpus(t, c, "strength.fit")
	path := fmt.Sprintf("/api/activities/%d/sets", id)

	stored, err := q.ListStrengthSets(t.Context(), db.ListStrengthSetsParams{UserID: me.Id, ActivityID: id, After: -1, MaxRows: 1000})
	if err != nil || len(stored) == 0 {
		t.Fatalf("stored sets = %d, %v", len(stored), err)
	}
	n := pageAll(t, c, path, func(p StrengthSetPage) int { return len(p.Items) }, func(p StrengthSetPage) *string { return p.NextCursor })
	if n != len(stored) {
		t.Errorf("paged %d sets, want %d", n, len(stored))
	}
	p := decodeJSON[StrengthSetPage](t, c.do(http.MethodGet, path+"?limit=100", nil, nil), http.StatusOK)
	named := 0
	for _, st := range p.Items {
		if st.Active && st.ExerciseName != nil {
			named++
		}
	}
	if named == 0 {
		t.Errorf("no active set carries an exercise name: %+v", p.Items[0])
	}
}

func TestMultisportLegs(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "UTC")
	ctx := t.Context()
	group, err := q.UpsertMultisportGroup(ctx, db.UpsertMultisportGroupParams{UserID: me.Id, Source: "upload", SourceGroupID: "tri"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 6, 1, 6, 0, 0, 0, time.UTC)
	var ids []int64
	for i, sport := range []string{"open_water_swimming", "transition", "cycling"} {
		id, err := q.UpsertActivity(ctx, db.UpsertActivityParams{
			UserID: me.Id, Source: "upload", SourceActivityID: fmt.Sprintf("tri/%d", i), Sport: sport, Subtype: "outdoor",
			StartTime: start.Add(time.Duration(i) * time.Hour), MultisportGroupID: &group,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	a := decodeJSON[ActivityDetail](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d", ids[1]), nil, nil), http.StatusOK)
	if len(a.Legs) != 3 || a.Legs[0].Id != ids[0] || a.Legs[1].Sport != ActivitySportTransition || a.Legs[2].Id != ids[2] || a.Workout != nil {
		t.Errorf("legs = %+v", a.Legs)
	}
}

func TestGetActivityStreams(t *testing.T) {
	t.Parallel()
	c, q, me := loggedIn(t, "UTC")
	id := uploadCorpus(t, c, "half_marathon.fit")
	path := fmt.Sprintf("/api/activities/%d/streams", id)
	stored, err := q.ListActivityStreams(t.Context(), db.ListActivityStreamsParams{UserID: me.Id, ActivityID: id, MaxRows: 1_000_000})
	if err != nil || len(stored) <= streamPageSize {
		t.Fatalf("stored %d samples, %v; the test needs more than a page", len(stored), err)
	}

	// Full resolution, page by page, with every channel aligned to the time axis.
	var times []float64
	url := path
	for range 100 {
		s := decodeJSON[ActivityStreams](t, c.do(http.MethodGet, url, nil, nil), http.StatusOK)
		if s.HrBpm == nil || len(*s.HrBpm) != len(s.TimeS) || s.Lat == nil || len(*s.Lat) != len(s.TimeS) {
			t.Fatalf("columns do not match the time axis")
		}
		times = append(times, s.TimeS...)
		if s.NextCursor == nil {
			break
		}
		url = path + "?cursor=" + *s.NextCursor
	}
	if len(times) != len(stored) || times[0] < 0 || times[0] > 5 {
		t.Errorf("paged %d samples from %v s, want %d from the start", len(times), times[0], len(stored))
	}
	for i := 1; i < len(times); i++ {
		if times[i] <= times[i-1] {
			t.Fatalf("time not ascending at %d", i)
		}
	}

	// Reduced: at most maxPoints per channel, endpoints kept.
	s := decodeJSON[ActivityStreams](t, c.do(http.MethodGet, path+"?maxPoints=100", nil, nil), http.StatusOK)
	if s.NextCursor != nil || len(s.TimeS) < 100 || len(s.TimeS) > 100*len(channels) {
		t.Errorf("reduced to %d samples", len(s.TimeS))
	}
	if s.TimeS[0] != times[0] || s.TimeS[len(s.TimeS)-1] != times[len(times)-1] {
		t.Errorf("endpoints %v..%v, want %v..%v", s.TimeS[0], s.TimeS[len(s.TimeS)-1], times[0], times[len(times)-1])
	}

	for _, q := range []string{"maxPoints=2", "maxPoints=10001", "cursor=bogus", "maxPoints=10&cursor=x"} {
		wantProblem(t, c.do(http.MethodGet, path+"?"+q, nil, nil), KindInvalidRequest)
	}
}

func TestDecimateWithoutChannels(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]db.ActivityStream, 50)
	for i := range rows {
		rows[i].Time = start.Add(time.Duration(i) * time.Second)
	}
	got := decimate(rows, start, 5)
	if len(got) != 5 || !got[0].Time.Equal(start) || !got[4].Time.Equal(rows[49].Time) {
		t.Errorf("kept %d samples", len(got))
	}
	if out := columns(got, start); out.HrBpm != nil || len(out.TimeS) != 5 {
		t.Errorf("columns = %+v", out)
	}
}

func TestSetActivityAnnotation(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	id := uploadCorpus(t, c, "intervals.fit")
	path := fmt.Sprintf("/api/activities/%d", id)

	// Pre-filled from the watch.
	a := decodeJSON[ActivityDetail](t, c.do(http.MethodGet, path, nil, nil), http.StatusOK)
	if a.Rpe == nil || a.Feel == nil || a.PainTags == nil || len(a.PainTags) != 0 {
		t.Fatalf("watch values: rpe %v, feel %v, tags %v", a.Rpe, a.Feel, a.PainTags)
	}

	cond := ActivityAnnotationWeatherConditionRain
	tags := []string{"  Left Knee ", "left knee", "", "Achilles"}
	a = decodeJSON[ActivityDetail](t, c.do(http.MethodPut, path+"/annotation", ActivityAnnotation{
		Rpe: new(float32(7.5)), Feel: new(25), Notes: new("Heavy legs"), PainTags: &tags,
		WeatherTemperatureC: new(float32(4)), WeatherCondition: &cond,
	}, nil), http.StatusOK)
	if *a.Rpe != 7.5 || *a.Feel != 25 || *a.Notes != "Heavy legs" || fmt.Sprint(a.PainTags) != "[left knee achilles]" ||
		*a.WeatherTemperatureC != 4 || *a.WeatherCondition != ActivityDetailWeatherConditionRain {
		t.Errorf("after editing: %+v", a)
	}
	if got := decodeJSON[ActivityDetail](t, c.do(http.MethodGet, path, nil, nil), http.StatusOK); fmt.Sprint(got.PainTags) != "[left knee achilles]" {
		t.Errorf("stored tags = %v", got.PainTags)
	}

	// The list filters by tag, case-insensitively.
	if page := decodeJSON[ActivityPage](t, c.do(http.MethodGet, "/api/activities?tag=LEFT%20KNEE", nil, nil), http.StatusOK); len(page.Items) != 1 {
		t.Errorf("tag filter: %d activities", len(page.Items))
	}
	if page := decodeJSON[ActivityPage](t, c.do(http.MethodGet, "/api/activities?tag=back", nil, nil), http.StatusOK); len(page.Items) != 0 {
		t.Errorf("unknown tag: %d activities", len(page.Items))
	}

	// Clearing: every field left out goes, blank notes included.
	a = decodeJSON[ActivityDetail](t, c.do(http.MethodPut, path+"/annotation", ActivityAnnotation{Notes: new("   ")}, nil), http.StatusOK)
	if a.Rpe != nil || a.Feel != nil || a.Notes != nil || len(a.PainTags) != 0 || a.WeatherTemperatureC != nil || a.WeatherCondition != nil {
		t.Errorf("after clearing: %+v", a)
	}

	tooMany := make([]string, maxPainTags+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprint("tag", i)
	}
	bad := ActivityAnnotationWeatherCondition("fog")
	for name, body := range map[string]ActivityAnnotation{
		"rpe":       {Rpe: new(float32(10.5))},
		"feel":      {Feel: new(101)},
		"notes":     {Notes: new(strings.Repeat("x", maxNotesLen+1))},
		"tags":      {PainTags: &tooMany},
		"long tag":  {PainTags: &[]string{strings.Repeat("k", maxPainTagLen+1)}},
		"cold":      {WeatherTemperatureC: new(float32(-61))},
		"condition": {WeatherCondition: &bad},
	} {
		rec := c.do(http.MethodPut, path+"/annotation", body, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}

	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	wantProblem(t, bob.do(http.MethodPut, path+"/annotation", ActivityAnnotation{Notes: new("mine")}, nil), KindNotFound)
}
