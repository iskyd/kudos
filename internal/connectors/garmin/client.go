// Package garmin is the connector for Garmin's official Personal API.
//
// Requests are sequential and read-only. A 429, a 5xx or a network error is
// retried with exponential backoff and jitter; a 401 re-mints the token from the
// key once. Endpoints come from Garmin's Personal API OpenAPI spec and its
// official client, garmin/connect-personal-api-python.
package garmin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iskyd/kudos/internal/connectors"
	"github.com/iskyd/kudos/internal/secret"
)

const (
	// DefaultBaseURL is the Personal API host.
	DefaultBaseURL = "https://personal.connectapi.garmin.com"
	tokenPath      = "/personalapitoken-service/personal-api/personal-api-key/token"
	// renewBefore re-mints a token with less than this left, as the official client does.
	renewBefore = time.Hour
	maxAttempts = 5
	baseDelay   = 2 * time.Second
	maxDelay    = time.Minute
	maxBody     = 64 << 20 // matches the upload limit
)

// Client calls the Personal API. Each call runs its requests one at a time.
type Client struct {
	baseURL string
	http    *http.Client
	now     func() time.Time
	sleep   func(context.Context, time.Duration) error
}

// New returns a client for baseURL (DefaultBaseURL in production).
func New(baseURL string, hc *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: hc, now: time.Now, sleep: sleep}
}

var _ connectors.Connector = (*Client)(nil)

// ID implements connectors.Connector.
func (*Client) ID() string { return "garmin" }

// Authenticate exchanges the key for a 24-hour token.
func (c *Client) Authenticate(ctx context.Context, creds connectors.Credentials) (*connectors.Session, error) {
	s := &connectors.Session{Credentials: creds}
	if err := c.mint(ctx, s); err != nil {
		return nil, err
	}
	return s, nil
}

func (c *Client) mint(ctx context.Context, s *connectors.Session) error {
	body, err := c.send(ctx, http.MethodPost, tokenPath, nil, "ApiKey "+s.Credentials.Key.Reveal())
	if errors.Is(err, errUnauthorized) {
		return fmt.Errorf("minting a token: %w", connectors.ErrKeyRejected)
	}
	if err != nil {
		return fmt.Errorf("minting a token: %w", err)
	}
	token := strings.TrimSpace(string(body))
	exp, err := expiry(token)
	if err != nil {
		return fmt.Errorf("minting a token: %w: %w", connectors.ErrUpstream, err)
	}
	s.Token, s.Expires = secret.New(token), exp
	return nil
}

// expiry reads the exp claim of a JWT without verifying it; Garmin verifies its own tokens.
func expiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("decoding the token: %w", err)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, errors.New("token has no exp claim")
	}
	return time.Unix(claims.Exp, 0).UTC(), nil
}

// get calls a data endpoint, renewing the token when it is near expiry and
// re-minting it once on a 401.
func (c *Client) get(ctx context.Context, s *connectors.Session, path string, query url.Values) ([]byte, error) {
	if s.Token.Reveal() == "" || s.Expires.Sub(c.now()) < renewBefore {
		if err := c.mint(ctx, s); err != nil {
			return nil, err
		}
	}
	body, err := c.send(ctx, http.MethodGet, path, query, "Bearer "+s.Token.Reveal())
	if errors.Is(err, errUnauthorized) {
		if err := c.mint(ctx, s); err != nil {
			return nil, err
		}
		body, err = c.send(ctx, http.MethodGet, path, query, "Bearer "+s.Token.Reveal())
		if errors.Is(err, errUnauthorized) {
			return nil, fmt.Errorf("GET %s: %w", path, connectors.ErrKeyRejected)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	return body, nil
}

func (c *Client) getJSON(ctx context.Context, s *connectors.Session, path string, query url.Values, v any) error {
	body, err := c.get(ctx, s, path, query)
	if err != nil {
		return err
	}
	if len(body) == 0 { // 204: no data for the range
		return nil
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("GET %s: %w: %w", path, connectors.ErrUpstream, err)
	}
	return nil
}

var errUnauthorized = errors.New("unauthorized")

// send performs one request, retrying 429, 5xx and network errors with
// exponential backoff and jitter. It returns errUnauthorized on 401 or 403.
func (c *Client) send(ctx context.Context, method, path string, query url.Values, auth string) ([]byte, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var last error
	for attempt := range maxAttempts {
		if attempt > 0 {
			if err := c.sleep(ctx, backoff(attempt, last)); err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, u, nil)
		if err != nil {
			return nil, fmt.Errorf("building the request: %w", err)
		}
		req.Header.Set("Authorization", auth)
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("calling Garmin: %w", ctx.Err())
			}
			last = fmt.Errorf("%w: %w", connectors.ErrUpstream, err)
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		_ = resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return nil, errUnauthorized
		case resp.StatusCode == http.StatusTooManyRequests:
			last = &retryError{err: connectors.ErrRateLimited, after: retryAfter(resp.Header)}
			continue
		case resp.StatusCode >= 500:
			last = &retryError{err: fmt.Errorf("%w: status %d", connectors.ErrUpstream, resp.StatusCode), after: retryAfter(resp.Header)}
			continue
		case resp.StatusCode >= 300:
			return nil, fmt.Errorf("%w: status %d", connectors.ErrUpstream, resp.StatusCode)
		case err != nil:
			last = fmt.Errorf("%w: reading the response: %w", connectors.ErrUpstream, err)
			continue
		case len(body) > maxBody:
			return nil, fmt.Errorf("%w: response over %d bytes", connectors.ErrUpstream, maxBody)
		}
		return body, nil
	}
	return nil, fmt.Errorf("after %d attempts: %w", maxAttempts, last)
}

// retryError carries the server's Retry-After, if any.
type retryError struct {
	err   error
	after time.Duration
}

func (e *retryError) Error() string { return e.err.Error() }
func (e *retryError) Unwrap() error { return e.err }

// backoff returns the delay before the given retry: Retry-After when the server
// sent one (capped), otherwise a random delay between half and all of
// base·2^(attempt−1), capped.
func backoff(attempt int, last error) time.Duration {
	var re *retryError
	if errors.As(last, &re) && re.after > 0 {
		return min(re.after, maxDelay)
	}
	ceiling := min(baseDelay<<(attempt-1), maxDelay)
	return ceiling/2 + rand.N(ceiling/2+1) //nolint:gosec // G404: jitter, not security
}

func retryAfter(h http.Header) time.Duration {
	secs, err := strconv.Atoi(h.Get("Retry-After"))
	if err != nil || secs < 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("waiting to retry: %w", ctx.Err())
	case <-t.C:
		return nil
	}
}
