package connectors

import (
	"context"
	"errors"
	"time"

	"github.com/iskyd/kudos/internal/secret"
)

// Errors callers branch on. Implementations wrap them with detail.
var (
	// ErrKeyRejected means the source refused the credentials; sync pauses until
	// the user enters new ones.
	ErrKeyRejected = errors.New("credentials rejected")
	// ErrRateLimited means the source still refused requests after backing off.
	ErrRateLimited = errors.New("rate limited")
	// ErrUpstream means the source failed or returned something unusable.
	ErrUpstream = errors.New("upstream error")
)

// Credentials are what the user gives KudOS to reach a source.
type Credentials struct {
	Key secret.Value[string]
}

// Session is an authenticated session. Connectors renew the token in place, so
// callers pass a pointer and persist Token and Expires after each call.
type Session struct {
	Credentials Credentials
	Token       secret.Value[string]
	Expires     time.Time
}

// RawActivity is an activity as listed by a source, before its file is parsed.
type RawActivity struct {
	SourceID string
	Start    time.Time // UTC
	// LocalOffset is the source's local-time offset, the fallback when the file
	// has none (invariant 9); nil when the source gives none.
	LocalOffset *time.Duration
}

// File is an activity file as recorded by the device.
type File struct {
	Name string
	Data []byte
}

// DailyMetrics are one day's health values in SI units (hydration in litres).
// Nil means the source has no value for that day.
type DailyMetrics struct {
	Day            time.Time // local calendar day, at midnight UTC
	RestingHRBpm   *int
	HRVMs          *float64
	SleepS         *int
	SleepDeepS     *int
	SleepLightS    *int
	SleepRemS      *int
	SleepAwakeS    *int
	SleepScore     *int
	Stress         *int
	BodyBatteryMax *int
	BodyBatteryMin *int
	Steps          *int
	WeightKg       *float64
	BodyFatPct     *float64
	BodyWaterPct   *float64
	MuscleMassKg   *float64
	BoneMassKg     *float64
	Spo2Pct        *float64
	HydrationL     *float64
}

// Connector is a data source. Date ranges are inclusive local calendar days.
type Connector interface {
	ID() string
	Authenticate(ctx context.Context, creds Credentials) (*Session, error)
	FetchActivities(ctx context.Context, s *Session, from, to time.Time) ([]RawActivity, error)
	DownloadActivityFile(ctx context.Context, s *Session, sourceID string) (File, error)
	FetchDailyMetrics(ctx context.Context, s *Session, from, to time.Time) ([]DailyMetrics, error)
}
