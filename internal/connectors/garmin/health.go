package garmin

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/iskyd/kudos/internal/connectors"
)

// maxRangeDays is the longest range the sleep and hydration endpoints accept.
const maxRangeDays = 31

type dailySummaries struct {
	Data []struct {
		CalendarDate string `json:"calendarDate"`
		HeartRate    struct {
			RestingValue *int `json:"restingValue"`
		} `json:"heartRate"`
		Movement struct {
			Steps struct {
				Value *int `json:"value"`
			} `json:"steps"`
		} `json:"movement"`
		Stress struct {
			AvgLevel *int `json:"avgLevel"`
		} `json:"stress"`
		BodyBattery struct {
			MaxValue *int `json:"maxValue"`
			MinValue *int `json:"minValue"`
		} `json:"bodyBattery"`
		PulseOx struct {
			AvgValue *float64 `json:"avgValue"`
		} `json:"pulseOx"`
		Hydration struct {
			ConsumedInFractionalMl *float64 `json:"consumedInFractionalMl"`
		} `json:"hydration"`
	} `json:"data"`
}

type sleepSummary struct {
	CalendarDate      string `json:"calendarDate"`
	SleepTimeSeconds  *int   `json:"sleepTimeSeconds"`
	DeepSleepSeconds  *int   `json:"deepSleepSeconds"`
	LightSleepSeconds *int   `json:"lightSleepSeconds"`
	RemSleepSeconds   *int   `json:"remSleepSeconds"`
	AwakeSleepSeconds *int   `json:"awakeSleepSeconds"`
	SleepScores       struct {
		Overall struct {
			Value *int `json:"value"`
		} `json:"overall"`
	} `json:"sleepScores"`
}

type hrvSummaries struct {
	HrvSummaries []struct {
		CalendarDate string   `json:"calendarDate"`
		LastNightAvg *float64 `json:"lastNightAvg"`
	} `json:"hrvSummaries"`
}

type weightRange struct {
	DailyWeightSummaries []struct {
		SummaryDate  string `json:"summaryDate"`
		LatestWeight *struct {
			// shortcut: units follow the spec's DateWeightDTO (weight in kg); no recording has a weight
			// entry yet, so re-check against one before relying on them.
			Weight     *float64 `json:"weight"`
			BodyFat    *float64 `json:"bodyFat"`
			BodyWater  *float64 `json:"bodyWater"`
			BoneMass   *float64 `json:"boneMass"`
			MuscleMass *float64 `json:"muscleMass"`
		} `json:"latestWeight"`
	} `json:"dailyWeightSummaries"`
}

// FetchDailyMetrics returns the health values of the local days from..to that
// have any, oldest first. Ranges are fetched in chunks the endpoints accept.
func (c *Client) FetchDailyMetrics(ctx context.Context, s *connectors.Session, from, to time.Time) ([]connectors.DailyMetrics, error) {
	days := map[string]*connectors.DailyMetrics{}
	day := func(date string) (*connectors.DailyMetrics, error) {
		if m, ok := days[date]; ok {
			return m, nil
		}
		d, err := time.Parse(time.DateOnly, date)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid calendar date %q", connectors.ErrUpstream, date)
		}
		days[date] = &connectors.DailyMetrics{Day: d}
		return days[date], nil
	}
	for start := from; !start.After(to); start = start.AddDate(0, 0, maxRangeDays) {
		end := start.AddDate(0, 0, maxRangeDays-1)
		if end.After(to) {
			end = to
		}
		if err := c.fetchRange(ctx, s, start.Format(time.DateOnly), end.Format(time.DateOnly), day); err != nil {
			return nil, err
		}
	}
	out := make([]connectors.DailyMetrics, 0, len(days))
	for _, m := range days {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day.Before(out[j].Day) })
	return out, nil
}

func (c *Client) fetchRange(ctx context.Context, s *connectors.Session, from, to string,
	day func(string) (*connectors.DailyMetrics, error),
) error {
	dates := url.Values{"startDate": {from}, "endDate": {to}}

	var summaries dailySummaries
	if err := c.getJSON(ctx, s, "/usersummary-service/v2/dailysummaries", dates, &summaries); err != nil {
		return err
	}
	for _, d := range summaries.Data {
		m, err := day(d.CalendarDate)
		if err != nil {
			return err
		}
		m.RestingHRBpm = positive(d.HeartRate.RestingValue)
		m.Steps = nonNegative(d.Movement.Steps.Value)
		m.Stress = nonNegative(d.Stress.AvgLevel)
		m.BodyBatteryMax = nonNegative(d.BodyBattery.MaxValue)
		m.BodyBatteryMin = nonNegative(d.BodyBattery.MinValue)
		m.Spo2Pct = positiveF(d.PulseOx.AvgValue)
		if ml := nonNegativeF(d.Hydration.ConsumedInFractionalMl); ml != nil {
			m.HydrationL = ptr(*ml / 1000)
		}
	}

	var sleeps []sleepSummary
	if err := c.getJSON(ctx, s, "/sleep-service/sleep/dailySleepSummariesByDate", dates, &sleeps); err != nil {
		return err
	}
	for _, d := range sleeps {
		m, err := day(d.CalendarDate)
		if err != nil {
			return err
		}
		m.SleepS, m.SleepDeepS = nonNegative(d.SleepTimeSeconds), nonNegative(d.DeepSleepSeconds)
		m.SleepLightS, m.SleepRemS = nonNegative(d.LightSleepSeconds), nonNegative(d.RemSleepSeconds)
		m.SleepAwakeS, m.SleepScore = nonNegative(d.AwakeSleepSeconds), nonNegative(d.SleepScores.Overall.Value)
	}

	var hrv hrvSummaries
	if err := c.getJSON(ctx, s, "/hrv-service/hrv/daily/"+from+"/"+to, nil, &hrv); err != nil {
		return err
	}
	for _, d := range hrv.HrvSummaries {
		m, err := day(d.CalendarDate)
		if err != nil {
			return err
		}
		m.HRVMs = positiveF(d.LastNightAvg)
	}

	var weights weightRange
	if err := c.getJSON(ctx, s, "/weight-service/weight/range/"+from+"/"+to, nil, &weights); err != nil {
		return err
	}
	for _, d := range weights.DailyWeightSummaries {
		if d.LatestWeight == nil {
			continue
		}
		m, err := day(d.SummaryDate)
		if err != nil {
			return err
		}
		w := d.LatestWeight
		m.WeightKg, m.BodyFatPct, m.BodyWaterPct = positiveF(w.Weight), positiveF(w.BodyFat), positiveF(w.BodyWater)
		m.MuscleMassKg, m.BoneMassKg = positiveF(w.MuscleMass), positiveF(w.BoneMass)
	}
	return nil
}

// Garmin writes negative values (-1, -2) when it has too little data; they mean "no value".
func nonNegative(v *int) *int {
	if v == nil || *v < 0 {
		return nil
	}
	return v
}

func positive(v *int) *int {
	if v == nil || *v <= 0 {
		return nil
	}
	return v
}

func nonNegativeF(v *float64) *float64 {
	if v == nil || *v < 0 {
		return nil
	}
	return v
}

func positiveF(v *float64) *float64 {
	if v == nil || *v <= 0 {
		return nil
	}
	return v
}

func ptr[T any](v T) *T { return &v }
