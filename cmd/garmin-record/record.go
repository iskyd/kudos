package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/iskyd/kudos/internal/anonymize"
)

// tokenPath is the key-to-token exchange, from Garmin's Personal API FAQ and its
// official Python client (garmin/connect-personal-api-python).
const tokenPath = "/personalapitoken-service/personal-api/personal-api-key/token"

// entry is one recorded call in index.json.
type entry struct {
	Name        string            `json:"name"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Status      int               `json:"status"`
	ContentType string            `json:"contentType,omitempty"`
	Bytes       int               `json:"bytes"`
	File        string            `json:"file,omitempty"`
	RateLimit   map[string]string `json:"rateLimitHeaders,omitempty"`
	// Token response: its claim names and lifetime, never the token.
	TokenClaims   []string `json:"tokenClaims,omitempty"`
	TokenLifetime string   `json:"tokenLifetime,omitempty"`
	// Activity download: the ZIP's entries after anonymizing.
	ZipEntries []zipEntry `json:"zipEntries,omitempty"`
}

type zipEntry struct {
	Name  string `json:"name"`
	Bytes int    `json:"bytes"`
}

// response is a read and closed HTTP response.
type response struct {
	status int
	header http.Header
	body   []byte
}

type recorder struct {
	client *http.Client
	cfg    config
	token  string
	red    *redactor
	index  []entry
	log    io.Writer
}

func record(ctx context.Context, client *http.Client, cfg config, key string, log io.Writer) error {
	if err := os.MkdirAll(cfg.outDir, 0o750); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	r := &recorder{client: client, cfg: cfg, red: newRedactor(), log: log}
	if err := r.mintToken(ctx, key); err != nil {
		return err
	}

	from, to := cfg.from.Format(dateLayout), cfg.to.Format(dateLayout)
	search, err := r.json(ctx, "activities_search", "/activitylist-service/activities/search/activities",
		url.Values{"start": {"0"}, "limit": {"20"}, "startDate": {from}, "endDate": {to}})
	if err != nil {
		return err
	}
	for i, id := range activityIDs(search, cfg.activities) {
		name := fmt.Sprintf("activity_%d", i+1)
		r.red.activityID(id) // maps it before its first appearance in a path or file name
		base := "/activity-service/activity/" + id
		if _, err := r.json(ctx, name, base, nil); err != nil {
			return err
		}
		if _, err := r.json(ctx, name+"_workouts", base+"/workouts", nil); err != nil {
			return err
		}
		if err := r.download(ctx, name, id); err != nil {
			return err
		}
	}

	// Health data KudOS reads (spec: Garmin Connect sync › Scope), from the Personal API OpenAPI spec.
	for _, c := range []struct {
		name, path string
		query      url.Values
	}{
		{"daily_summaries", "/usersummary-service/v2/dailysummaries", url.Values{"startDate": {from}, "endDate": {to}}},
		{"sleep_summaries", "/sleep-service/sleep/dailySleepSummariesByDate", url.Values{"startDate": {from}, "endDate": {to}}},
		{"sleep_day", "/sleep-service/sleep/dailySleep", url.Values{"date": {to}}},
		{"hrv_summaries", "/hrv-service/hrv/daily/" + from + "/" + to, nil},
		{"hrv_day", "/hrv-service/hrv/" + to, nil},
		{"stress", "/usersummary-service/stats/stress/daily/" + from + "/" + to, nil},
		{"body_battery", "/wellness-service/wellness/bodyBattery/reports/daily", url.Values{"startDate": {from}, "endDate": {to}}},
		{"steps", "/usersummary-service/stats/steps/daily/" + from + "/" + to, nil},
		{"heart_rate_day", "/wellness-service/wellness/dailyHeartRate", url.Values{"date": {to}}},
		{"weight", "/weight-service/weight/range/" + from + "/" + to, url.Values{"includeAll": {"true"}}},
		{"hydration", "/usersummary-service/stats/hydration/daily/" + from + "/" + to, nil},
		{"spo2_day", "/wellness-service/wellness/daily/spo2acclimation/" + to, nil},
		{"heart_rate_zones", "/biometric-service/heartRateZones/v2", nil},
	} {
		if _, err := r.json(ctx, c.name, c.path, c.query); err != nil {
			return err
		}
	}

	if err := r.red.checkLeaks(r.cfg.outDir, r.files()); err != nil {
		return err
	}
	index, err := json.MarshalIndent(r.index, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding index: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.outDir, "index.json"), append(index, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing index: %w", err)
	}
	fmt.Fprintf(log, "recorded %d calls into %s; review the files before committing them\n", len(r.index), cfg.outDir)
	return nil
}

// mintToken exchanges the key for a bearer token. A failure stops the recording.
func (r *recorder) mintToken(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.baseURL+tokenPath, nil)
	if err != nil {
		return fmt.Errorf("building token request: %w", err)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Authorization", "ApiKey "+key)
	resp, err := r.do(req)
	if err != nil {
		return err
	}
	e := entry{Name: "token", Method: http.MethodPost, Path: tokenPath, Status: resp.status,
		ContentType: resp.header.Get("Content-Type"), Bytes: len(resp.body), RateLimit: rateLimit(resp.header)}
	if resp.status/100 != 2 {
		r.add(e)
		return fmt.Errorf("token request returned HTTP %d; check the key", resp.status)
	}
	r.token = strings.TrimSpace(string(resp.body))
	e.TokenClaims, e.TokenLifetime = describeJWT(r.token)
	r.add(e)
	return nil
}

// json GETs a JSON endpoint, saves the redacted body and returns it decoded.
// Error statuses are recorded and do not stop the recording.
func (r *recorder) json(ctx context.Context, name, p string, query url.Values) (any, error) {
	resp, err := r.get(ctx, p, query, "application/json")
	if err != nil {
		return nil, err
	}
	e := entry{Name: name, Method: http.MethodGet, Path: r.red.path(p, query), Status: resp.status,
		ContentType: resp.header.Get("Content-Type"), Bytes: len(resp.body), RateLimit: rateLimit(resp.header)}
	var decoded any
	if len(resp.body) > 0 {
		dec := json.NewDecoder(bytes.NewReader(resp.body))
		dec.UseNumber()
		if dec.Decode(&decoded) == nil {
			out, err := json.MarshalIndent(r.red.value("", decoded), "", "  ")
			if err != nil {
				return nil, fmt.Errorf("encoding %s: %w", name, err)
			}
			e.File = name + ".json"
			if err := os.WriteFile(filepath.Join(r.cfg.outDir, e.File), append(out, '\n'), 0o600); err != nil {
				return nil, fmt.Errorf("writing %s: %w", e.File, err)
			}
		}
	}
	r.add(e)
	return decoded, nil
}

// download saves an activity's original-file ZIP with its FIT files anonymized.
func (r *recorder) download(ctx context.Context, name, id string) error {
	p := "/download-service/files/activity/" + id
	resp, err := r.get(ctx, p, nil, "application/zip, application/x-zip-compressed, application/octet-stream")
	if err != nil {
		return err
	}
	e := entry{Name: name + "_download", Method: http.MethodGet, Path: r.red.path(p, nil), Status: resp.status,
		ContentType: resp.header.Get("Content-Type"), Bytes: len(resp.body), RateLimit: rateLimit(resp.header)}
	if resp.status/100 == 2 {
		out, entries, err := r.anonymizeZip(resp.body)
		if err != nil {
			return fmt.Errorf("%s download: %w", name, err)
		}
		e.File, e.ZipEntries = name+".zip", entries
		if err := os.WriteFile(filepath.Join(r.cfg.outDir, e.File), out, 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", e.File, err)
		}
	}
	r.add(e)
	return nil
}

func (r *recorder) anonymizeZip(data []byte) ([]byte, []zipEntry, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, fmt.Errorf("reading zip: %w", err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var entries []zipEntry
	for _, f := range zr.File {
		content, err := readZipFile(f)
		if err != nil {
			return nil, nil, err
		}
		switch strings.ToLower(path.Ext(f.Name)) {
		case ".fit":
			var out bytes.Buffer
			if _, err := anonymize.FIT(bytes.NewReader(content), &out); err != nil {
				return nil, nil, fmt.Errorf("anonymizing %s: %w", path.Ext(f.Name), err)
			}
			content = out.Bytes()
		case ".gpx", ".tcx":
			if content, _, err = anonymize.XML(content); err != nil {
				return nil, nil, fmt.Errorf("anonymizing %s: %w", path.Ext(f.Name), err)
			}
		default:
			return nil, nil, fmt.Errorf("unexpected %q file in the zip; not recorded", path.Ext(f.Name))
		}
		entryName := r.red.text(f.Name)
		w, err := zw.Create(entryName)
		if err != nil {
			return nil, nil, fmt.Errorf("writing zip: %w", err)
		}
		if _, err := w.Write(content); err != nil {
			return nil, nil, fmt.Errorf("writing zip: %w", err)
		}
		entries = append(entries, zipEntry{Name: entryName, Bytes: len(content)})
	}
	if err := zw.Close(); err != nil {
		return nil, nil, fmt.Errorf("writing zip: %w", err)
	}
	return buf.Bytes(), entries, nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("opening zip entry: %w", err)
	}
	defer rc.Close()
	// Activity files are far below this; the limit guards against zip bombs.
	b, err := io.ReadAll(io.LimitReader(rc, 256<<20))
	if err != nil {
		return nil, fmt.Errorf("reading zip entry: %w", err)
	}
	return b, nil
}

func (r *recorder) get(ctx context.Context, p string, query url.Values, accept string) (response, error) {
	u := r.cfg.baseURL + p
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return response{}, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("Authorization", "Bearer "+r.token)
	return r.do(req)
}

// do sends one request after the configured pause, so calls stay sequential and slow.
func (r *recorder) do(req *http.Request) (response, error) {
	if len(r.index) > 0 {
		select {
		case <-time.After(r.cfg.delay):
		case <-req.Context().Done():
			return response{}, fmt.Errorf("interrupted: %w", req.Context().Err())
		}
	}
	resp, err := r.client.Do(req)
	if err != nil {
		// Reported without the underlying error, whose URL could carry query values.
		return response{}, fmt.Errorf("%s %s: request failed", req.Method, req.URL.Path)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return response{}, fmt.Errorf("reading %s response: %w", req.URL.Path, err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: body}, nil
}

func (r *recorder) add(e entry) {
	r.index = append(r.index, e)
	fmt.Fprintf(r.log, "%-22s %d %7d bytes\n", e.Name, e.Status, e.Bytes)
}

func (r *recorder) files() []string {
	var files []string
	for _, e := range r.index {
		if e.File != "" {
			files = append(files, e.File)
		}
	}
	return files
}

// activityIDs returns up to n activity IDs from a search response, as decimal strings.
func activityIDs(search any, n int) []string {
	list, _ := search.([]any)
	var ids []string
	for _, item := range list {
		obj, _ := item.(map[string]any)
		if id, ok := obj["activityId"].(json.Number); ok && len(ids) < n {
			ids = append(ids, id.String())
		}
	}
	return ids
}

// rateLimit returns the response headers that describe rate limiting.
func rateLimit(h http.Header) map[string]string {
	out := map[string]string{}
	for name, values := range h {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "ratelimit") || strings.Contains(lower, "rate-limit") || lower == "retry-after" {
			out[name] = strings.Join(values, ", ")
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// describeJWT returns a token's claim names and remaining lifetime, never its values.
func describeJWT(token string) ([]string, string) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, "not a JWT"
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil, "unreadable payload"
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, "unreadable payload"
	}
	names := make([]string, 0, len(claims))
	for name := range claims {
		names = append(names, name)
	}
	slices.Sort(names)
	exp, ok := claims["exp"].(float64)
	if !ok {
		return names, "no exp claim"
	}
	return names, time.Until(time.Unix(int64(exp), 0)).Round(time.Minute).String()
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }
