package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

func createZones(t *testing.T, c *apiClient, in ZoneSetInput) ZoneSet {
	t.Helper()
	return decodeJSON[ZoneSet](t, c.do(http.MethodPost, "/api/zones", in, nil), http.StatusCreated)
}

func activityZones(t *testing.T, c *apiClient, id int64) ActivityZones {
	t.Helper()
	return decodeJSON[ActivityZones](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d/zones", id), nil, nil), http.StatusOK)
}

func on(d time.Time) openapi_types.Date { return openapi_types.Date{Time: d} }

func TestActivityUsesTheVersionValidOnItsDate(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	id := uploadCorpus(t, c, "intervals.fit")
	a := decodeJSON[ActivityDetail](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d", id), nil, nil), http.StatusOK)
	local := a.StartTime.Add(time.Duration(a.UtcOffsetS) * time.Second)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)

	if z := activityZones(t, c, id); z.ZoneSetId != nil || z.Zones.Hr != nil || !z.Day.Equal(day) || z.Sport != ZoneSportRunning {
		t.Errorf("without versions: %+v", z)
	}

	before := createZones(t, c, ZoneSetInput{Sport: ZoneSportRunning, EffectiveFrom: on(day.AddDate(0, 0, -30)), LthrBpm: new(176)})
	createZones(t, c, ZoneSetInput{Sport: ZoneSportRunning, EffectiveFrom: on(day.AddDate(0, 0, 1)), LthrBpm: new(171)})
	// Cycling's FTP never reaches a run.
	createZones(t, c, ZoneSetInput{Sport: ZoneSportCycling, EffectiveFrom: on(day.AddDate(-1, 0, 0)), FtpW: new(378)})
	z := activityZones(t, c, id)
	if z.ZoneSetId == nil || *z.ZoneSetId != before.Id || *z.Zones.HrModel != "friel_run_hr" || z.Zones.Power != nil {
		t.Fatalf("resolved: %+v", z)
	}
	// Friel Z2 starts at 85% of LTHR 176.
	if low := (*z.Zones.Hr)[1].Low; low == nil || *low != 176*0.85 {
		t.Errorf("Z2 starts at %v", low)
	}

	// A version effective on the day itself applies, with general max and resting HR left aside.
	same := createZones(t, c, ZoneSetInput{Sport: ZoneSportRunning, EffectiveFrom: on(day), ThresholdSpeedMS: new(1000 / 240.0)})
	createZones(t, c, ZoneSetInput{Sport: ZoneSportGeneral, EffectiveFrom: on(day), MaxHrBpm: new(190), RestingHrBpm: new(50)})
	z = activityZones(t, c, id)
	if *z.ZoneSetId != same.Id || *z.Zones.PaceModel != "friel_run_pace" || *z.Zones.HrModel != "heart_rate_reserve" || z.GeneralZoneSetId == nil {
		t.Errorf("same day: %+v", z)
	}

	// Deleting it falls back to the version before.
	if r := c.do(http.MethodDelete, fmt.Sprintf("/api/zones/%d", same.Id), nil, nil); r.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", r.Code)
	}
	if z := activityZones(t, c, id); *z.ZoneSetId != before.Id {
		t.Errorf("after delete: %+v", z)
	}
}

func TestSwimUsesCSSAndHeartRateReserve(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	id := uploadCorpus(t, c, "pool_swim.fit")
	createZones(t, c, ZoneSetInput{Sport: ZoneSportSwimming, EffectiveFrom: on(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)), CssMS: new(1.0)})
	createZones(t, c, ZoneSetInput{Sport: ZoneSportGeneral, EffectiveFrom: on(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)), MaxHrBpm: new(190), RestingHrBpm: new(50)})
	z := activityZones(t, c, id)
	if z.Sport != ZoneSportSwimming || *z.Zones.PaceModel != "css" || len(*z.Zones.Pace) != 5 || *z.Zones.HrModel != "heart_rate_reserve" {
		t.Errorf("swim zones: %+v", z.Zones)
	}
}

func TestZoneSetValidationAndPaging(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	d := on(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	for name, in := range map[string]ZoneSetInput{
		"sport":            {Sport: "rowing", EffectiveFrom: d, LthrBpm: new(170)},
		"running empty":    {Sport: ZoneSportRunning, EffectiveFrom: d},
		"cycling empty":    {Sport: ZoneSportCycling, EffectiveFrom: d, CssMS: new(1.0)},
		"swimming empty":   {Sport: ZoneSportSwimming, EffectiveFrom: d},
		"general empty":    {Sport: ZoneSportGeneral, EffectiveFrom: d, MaxHrBpm: new(190)},
		"resting alone":    {Sport: ZoneSportRunning, EffectiveFrom: d, LthrBpm: new(170), RestingHrBpm: new(50)},
		"resting too high": {Sport: ZoneSportGeneral, EffectiveFrom: d, MaxHrBpm: new(100), RestingHrBpm: new(100)},
		"lthr":             {Sport: ZoneSportRunning, EffectiveFrom: d, LthrBpm: new(300)},
		"ftp":              {Sport: ZoneSportCycling, EffectiveFrom: d, FtpW: new(0)},
		"speed":            {Sport: ZoneSportRunning, EffectiveFrom: d, ThresholdSpeedMS: new(0.0)},
		"css":              {Sport: ZoneSportSwimming, EffectiveFrom: d, CssMS: new(20.0)},
	} {
		t.Run(name, func(t *testing.T) {
			c.t = t
			wantProblem(t, c.do(http.MethodPost, "/api/zones", in, nil), KindInvalidRequest)
		})
	}
	c.t = t

	for i := range 3 {
		createZones(t, c, ZoneSetInput{Sport: ZoneSportCycling, EffectiveFrom: on(d.AddDate(0, i, 0)), FtpW: new(250 + i)})
	}
	wantProblem(t, c.do(http.MethodPost, "/api/zones", ZoneSetInput{Sport: ZoneSportCycling, EffectiveFrom: d, FtpW: new(200)}, nil), KindInvalidRequest)
	general := createZones(t, c, ZoneSetInput{Sport: ZoneSportGeneral, EffectiveFrom: d, MaxHrBpm: new(190), RestingHrBpm: new(50)})
	if *general.Zones.HrModel != "heart_rate_reserve" || general.LthrBpm != nil {
		t.Errorf("general version: %+v", general)
	}

	if n := pageAll(t, c, "/api/zones", func(p ZoneSetPage) int { return len(p.Items) }, func(p ZoneSetPage) *string { return p.NextCursor }); n != 4 {
		t.Errorf("paged %d versions", n)
	}
	page := decodeJSON[ZoneSetPage](t, c.do(http.MethodGet, "/api/zones?sport=cycling", nil, nil), http.StatusOK)
	if len(page.Items) != 3 || *page.Items[0].FtpW != 252 || (*page.Items[0].Zones.Power)[3].Name != "Threshold" {
		t.Errorf("cycling, latest first: %+v", page.Items)
	}
	for _, q := range []string{"sport=rowing", "limit=0", "cursor=bogus"} {
		wantProblem(t, c.do(http.MethodGet, "/api/zones?"+q, nil, nil), KindInvalidRequest)
	}

	// Bob sees none of Alice's versions and cannot delete them.
	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	if p := decodeJSON[ZoneSetPage](t, bob.do(http.MethodGet, "/api/zones", nil, nil), http.StatusOK); len(p.Items) != 0 {
		t.Errorf("bob lists %d", len(p.Items))
	}
	wantProblem(t, bob.do(http.MethodDelete, fmt.Sprintf("/api/zones/%d", general.Id), nil, nil), KindNotFound)
	run := uploadCorpus(t, c, "intervals.fit")
	wantProblem(t, bob.do(http.MethodGet, fmt.Sprintf("/api/activities/%d/zones", run), nil, nil), KindNotFound)
}
