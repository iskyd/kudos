// Command garmin-record records Garmin Personal API responses as test fixtures.
//
//	GARMIN_PERSONAL_API_KEY_FILE=~/.config/kudos/garmin_key go run ./cmd/garmin-record [flags]
//
// It exchanges the key for a token, calls every endpoint KudOS uses over a small
// date range, one request at a time, and writes the responses to the output
// directory with personal identifiers, names and coordinates redacted and the
// FIT files in activity downloads anonymized. index.json lists each call with its
// status, content type, size and any rate-limit headers. The key and the token are
// never written or printed. Review the output before committing it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://personal.connectapi.garmin.com"
	dateLayout     = time.DateOnly
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "garmin-record:", err)
		os.Exit(1)
	}
}

func run() error {
	yesterday := time.Now().AddDate(0, 0, -1).Format(dateLayout)
	var (
		out        = flag.String("out", "testdata/garmin", "output directory")
		from       = flag.String("from", "", "first day, YYYY-MM-DD (default: two days before -to)")
		to         = flag.String("to", yesterday, "last day, YYYY-MM-DD")
		activities = flag.Int("activities", 2, "how many activities of the range to record in full, including the file download")
		delay      = flag.Duration("delay", time.Second, "pause between requests")
	)
	flag.Parse()

	key, err := loadKey(os.Getenv)
	if err != nil {
		return err
	}
	cfg, err := newConfig(*from, *to, *activities)
	if err != nil {
		return err
	}
	cfg.baseURL = strings.TrimRight(or(os.Getenv("GARMIN_PERSONAL_API_BASE_URL"), defaultBaseURL), "/")
	cfg.outDir, cfg.delay = *out, *delay

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return record(ctx, &http.Client{Timeout: time.Minute}, cfg, key, os.Stdout)
}

// loadKey reads the key from GARMIN_PERSONAL_API_KEY, or else from the file named
// by GARMIN_PERSONAL_API_KEY_FILE, which keeps it out of the shell history.
func loadKey(getenv func(string) string) (string, error) {
	if key := strings.TrimSpace(getenv("GARMIN_PERSONAL_API_KEY")); key != "" {
		return key, nil
	}
	path := getenv("GARMIN_PERSONAL_API_KEY_FILE")
	if path == "" {
		return "", errors.New("set GARMIN_PERSONAL_API_KEY_FILE (or GARMIN_PERSONAL_API_KEY)")
	}
	b, err := os.ReadFile(path) //nolint:gosec // G703: the path is the operator's own setting
	if err != nil {
		return "", fmt.Errorf("reading the key file: %w", err)
	}
	key := strings.TrimSpace(string(b))
	if key == "" {
		return "", errors.New("the key file is empty")
	}
	return key, nil
}

type config struct {
	baseURL, outDir string
	from, to        time.Time
	activities      int
	delay           time.Duration
}

func newConfig(from, to string, activities int) (config, error) {
	end, err := time.Parse(dateLayout, to)
	if err != nil {
		return config{}, fmt.Errorf("-to: %w", err)
	}
	start := end.AddDate(0, 0, -2)
	if from != "" {
		if start, err = time.Parse(dateLayout, from); err != nil {
			return config{}, fmt.Errorf("-from: %w", err)
		}
	}
	if start.After(end) || end.Sub(start) > 31*24*time.Hour {
		return config{}, errors.New("-from must be up to 31 days before -to")
	}
	if activities < 0 || activities > 10 {
		return config{}, errors.New("-activities must be 0 to 10")
	}
	return config{from: start, to: end, activities: activities}, nil
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
