package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/iskyd/kudos/internal/connectors/garminsync"
	"github.com/iskyd/kudos/internal/crypto"
	"github.com/iskyd/kudos/internal/importer/ingest"
	"github.com/iskyd/kudos/internal/jobs"
	"github.com/iskyd/kudos/internal/store/db"
)

const (
	testPassword = "correct horse battery staple"
	testTileURL  = "https://tiles.example/{z}/{x}/{y}.png"
)

// testCipher uses a fixed key so tests can decrypt what handlers store.
func testCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	c, err := crypto.New(bytes.Repeat([]byte{7}, crypto.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// apiClient drives the router like a browser: it keeps the session cookie and
// sends the CSRF token on writes.
type apiClient struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
	csrf   string
}

func newAPI(t *testing.T, registration bool, logs *bytes.Buffer) (*apiClient, *db.Queries) {
	t.Helper()
	pool := pg.NewDB(t)
	q := db.New(pool)
	// The backfill worker is registered so jobs can be queued; tests never work them.
	jobClient, err := jobs.NewClient(pool, discard(), func(w *river.Workers) {
		river.AddWorker(w, &garminsync.Worker{})
		river.AddWorker(w, &garminsync.SyncWorker{})
	})
	if err != nil {
		t.Fatal(err)
	}
	cursors, err := NewCursors(bytes.Repeat([]byte{9}, crypto.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	logger := discard()
	if logs != nil {
		logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	h := NewRouter(logger, newServer(q, ingest.New(pool, jobClient, t.TempDir()), cursors, GarminDeps{
		Auth: fakeGarmin{}, Cipher: testCipher(t), Sync: garminsync.NewService(pool, jobClient),
	}, registration, testTileURL, cheapArgon()), spaStub)
	return &apiClient{t: t, h: h}, q
}

func (c *apiClient) do(method, path string, body any, header map[string]string) *httptest.ResponseRecorder {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatal(err)
		}
	}
	return c.send(method, path, &buf, "application/json", header)
}

// send makes a request with a raw body.
func (c *apiClient) send(method, path string, body io.Reader, contentType string, header map[string]string) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequestWithContext(c.t.Context(), method, path, body)
	req.Header.Set("Content-Type", contentType)
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.csrf != "" {
		req.Header.Set(csrfHeader, c.csrf)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == sessionCookieName {
			c.cookie = ck
		}
	}
	return rec
}

// session decodes a Me response and keeps its CSRF token.
func (c *apiClient) session(rec *httptest.ResponseRecorder, wantCode int) Me {
	c.t.Helper()
	if rec.Code != wantCode {
		c.t.Fatalf("status = %d, want %d: %s", rec.Code, wantCode, rec.Body)
	}
	var me Me
	if err := json.NewDecoder(rec.Body).Decode(&me); err != nil {
		c.t.Fatal(err)
	}
	c.csrf = me.CsrfToken
	return me
}

func wantProblem(t *testing.T, rec *httptest.ResponseRecorder, kind Kind) {
	t.Helper()
	if rec.Code != kind.status() {
		t.Fatalf("status = %d, want %d: %s", rec.Code, kind.status(), rec.Body)
	}
	if p := decodeProblem(t, rec); p.Type != "urn:kudos:problem:"+string(kind) {
		t.Fatalf("problem type = %s, want %s", p.Type, kind)
	}
}

func register(username string) RegisterRequest {
	return RegisterRequest{Username: username, Password: testPassword}
}

func TestRegister(t *testing.T) {
	t.Parallel()
	c, _ := newAPI(t, true, nil)

	me := c.session(c.do(http.MethodPost, "/api/auth/register", RegisterRequest{Username: "Alice", Password: testPassword, Timezone: new("Europe/Rome")}, nil), http.StatusCreated)
	if me.Username != "alice" || me.Timezone != "Europe/Rome" || me.Id == 0 || me.CsrfToken == "" {
		t.Fatalf("register = %+v", me)
	}
	ck := c.cookie
	if ck == nil || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Secure || ck.Path != "/" {
		t.Fatalf("session cookie = %+v, want HttpOnly, SameSite=Lax, not Secure over HTTP", ck)
	}
	if d := time.Until(ck.Expires); d < sessionTTL-time.Minute || d > sessionTTL {
		t.Fatalf("cookie expires in %v, want %v", d, sessionTTL)
	}
	// Registering logs in.
	if got := c.session(c.do(http.MethodGet, "/api/me", nil, nil), http.StatusOK); got != me {
		t.Fatalf("me = %+v, want %+v", got, me)
	}
}

func TestRegisterDefaultsAndSecureCookie(t *testing.T) {
	t.Parallel()
	c, _ := newAPI(t, true, nil)

	me := c.session(c.do(http.MethodPost, "/api/auth/register", register("bob"), map[string]string{"X-Forwarded-Proto": "https"}), http.StatusCreated)
	if me.Timezone != "UTC" {
		t.Fatalf("timezone = %q, want UTC", me.Timezone)
	}
	if !c.cookie.Secure {
		t.Fatal("cookie not Secure behind an HTTPS proxy")
	}
}

func TestRegisterRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		registration bool
		body         any
		want         Kind
	}{
		{name: "registration disabled", body: register("alice"), want: KindRegistrationDisabled},
		{name: "short username", registration: true, body: register("al"), want: KindInvalidRequest},
		{name: "long username", registration: true, body: register(strings.Repeat("a", 33)), want: KindInvalidRequest},
		{name: "username with space", registration: true, body: register("al ice"), want: KindInvalidRequest},
		{name: "short password", registration: true, body: RegisterRequest{Username: "alice", Password: "1234567"}, want: KindInvalidRequest},
		{name: "long password", registration: true, body: RegisterRequest{Username: "alice", Password: strings.Repeat("p", 257)}, want: KindInvalidRequest},
		{name: "unknown timezone", registration: true, body: RegisterRequest{Username: "alice", Password: testPassword, Timezone: new("Mars/Olympus")}, want: KindInvalidRequest},
		{name: "server timezone", registration: true, body: RegisterRequest{Username: "alice", Password: testPassword, Timezone: new("Local")}, want: KindInvalidRequest},
		{name: "not json", registration: true, body: "nope", want: KindInvalidRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := newAPI(t, tt.registration, nil)
			wantProblem(t, c.do(http.MethodPost, "/api/auth/register", tt.body, nil), tt.want)
			if c.cookie != nil {
				t.Fatal("rejected registration set a cookie")
			}
		})
	}
}

func TestRegisterDuplicateUsername(t *testing.T) {
	t.Parallel()
	c, _ := newAPI(t, true, nil)

	c.session(c.do(http.MethodPost, "/api/auth/register", register("alice"), nil), http.StatusCreated)
	// Usernames are case-insensitive.
	wantProblem(t, c.do(http.MethodPost, "/api/auth/register", register("ALICE"), nil), KindUsernameTaken)
}

func TestLogin(t *testing.T) {
	t.Parallel()
	c, _ := newAPI(t, true, nil)
	registered := c.session(c.do(http.MethodPost, "/api/auth/register", register("alice"), nil), http.StatusCreated)

	tests := []struct {
		name     string
		username string
		password string
		want     Kind
	}{
		{name: "wrong password", username: "alice", password: testPassword + "!", want: KindInvalidCredentials},
		{name: "unknown user", username: "mallory", password: testPassword, want: KindInvalidCredentials},
		{name: "unknown user with the dummy password", username: "mallory", password: "dummy password", want: KindInvalidCredentials},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			anon := &apiClient{t: t, h: c.h}
			wantProblem(t, anon.do(http.MethodPost, "/api/auth/login", LoginRequest{Username: tt.username, Password: tt.password}, nil), tt.want)
			if anon.cookie != nil {
				t.Fatal("failed login set a cookie")
			}
		})
	}

	fresh := &apiClient{t: t, h: c.h}
	me := fresh.session(fresh.do(http.MethodPost, "/api/auth/login", LoginRequest{Username: "Alice", Password: testPassword}, nil), http.StatusOK)
	if me.Id != registered.Id || me.CsrfToken == registered.CsrfToken {
		t.Fatalf("login = %+v, want same user with a new session", me)
	}
	// Both sessions are valid.
	c.session(c.do(http.MethodGet, "/api/me", nil, nil), http.StatusOK)
	fresh.session(fresh.do(http.MethodGet, "/api/me", nil, nil), http.StatusOK)
}

func TestSessionRequired(t *testing.T) {
	t.Parallel()
	c, q := newAPI(t, true, nil)
	me := c.session(c.do(http.MethodPost, "/api/auth/register", register("alice"), nil), http.StatusCreated)

	anon := &apiClient{t: t, h: c.h}
	wantProblem(t, anon.do(http.MethodGet, "/api/me", nil, nil), KindUnauthorized)

	unknown := &apiClient{t: t, h: c.h, cookie: &http.Cookie{Name: sessionCookieName, Value: "forged"}}
	wantProblem(t, unknown.do(http.MethodGet, "/api/me", nil, nil), KindUnauthorized)

	// An expired session is the same as none.
	if err := q.CreateSession(t.Context(), db.CreateSessionParams{
		TokenHash: hashToken("expired"), UserID: me.Id, CsrfToken: "c", ExpiresAt: time.Now().Add(-time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	expired := &apiClient{t: t, h: c.h, cookie: &http.Cookie{Name: sessionCookieName, Value: "expired"}, csrf: "c"}
	wantProblem(t, expired.do(http.MethodGet, "/api/me", nil, nil), KindUnauthorized)
	wantProblem(t, expired.do(http.MethodPost, "/api/auth/logout", nil, nil), KindUnauthorized)
}

func TestCSRF(t *testing.T) {
	t.Parallel()
	c, _ := newAPI(t, true, nil)
	me := c.session(c.do(http.MethodPost, "/api/auth/register", register("alice"), nil), http.StatusCreated)

	c.csrf = ""
	wantProblem(t, c.do(http.MethodPost, "/api/auth/logout", nil, nil), KindCSRF)
	c.csrf = "wrong"
	wantProblem(t, c.do(http.MethodPatch, "/api/me", UpdateMeRequest{Timezone: "Europe/Rome"}, nil), KindCSRF)

	// Reads need no token.
	c.csrf = ""
	c.session(c.do(http.MethodGet, "/api/me", nil, nil), http.StatusOK)

	// Browsers' cross-site writes are rejected before any handler, even with a token.
	c.csrf = me.CsrfToken
	wantProblem(t, c.do(http.MethodPatch, "/api/me", UpdateMeRequest{Timezone: "Europe/Rome"}, map[string]string{"Sec-Fetch-Site": "cross-site"}), KindCrossOrigin)
	anon := &apiClient{t: t, h: c.h}
	wantProblem(t, anon.do(http.MethodPost, "/api/auth/login", LoginRequest{Username: "alice", Password: testPassword}, map[string]string{"Sec-Fetch-Site": "cross-site"}), KindCrossOrigin)
}

func TestUpdateMe(t *testing.T) {
	t.Parallel()
	c, _ := newAPI(t, true, nil)
	me := c.session(c.do(http.MethodPost, "/api/auth/register", register("alice"), nil), http.StatusCreated)

	got := c.session(c.do(http.MethodPatch, "/api/me", UpdateMeRequest{Timezone: "America/New_York"}, nil), http.StatusOK)
	if got.Id != me.Id || got.Timezone != "America/New_York" || got.CsrfToken != me.CsrfToken {
		t.Fatalf("updateMe = %+v", got)
	}
	if got := c.session(c.do(http.MethodGet, "/api/me", nil, nil), http.StatusOK); got.Timezone != "America/New_York" {
		t.Fatalf("me after update = %+v", got)
	}
	wantProblem(t, c.do(http.MethodPatch, "/api/me", UpdateMeRequest{Timezone: "Nowhere"}, nil), KindInvalidRequest)
}

func TestLogout(t *testing.T) {
	t.Parallel()
	c, _ := newAPI(t, true, nil)
	c.session(c.do(http.MethodPost, "/api/auth/register", register("alice"), nil), http.StatusCreated)
	stolen := *c.cookie

	rec := c.do(http.MethodPost, "/api/auth/logout", nil, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d: %s", rec.Code, rec.Body)
	}
	if c.cookie.MaxAge >= 0 || c.cookie.Value != "" {
		t.Fatalf("logout cookie = %+v, want cleared", c.cookie)
	}
	// The old token is dead server-side, not only in this browser.
	replay := &apiClient{t: t, h: c.h, cookie: &stolen}
	wantProblem(t, replay.do(http.MethodGet, "/api/me", nil, nil), KindUnauthorized)
}

func TestSessionSlidingExpiry(t *testing.T) {
	t.Parallel()
	c, q := newAPI(t, true, nil)
	me := c.session(c.do(http.MethodPost, "/api/auth/register", register("alice"), nil), http.StatusCreated)
	ctx := t.Context()

	// Fresh sessions are not rewritten on every request.
	if rec := c.do(http.MethodGet, "/api/me", nil, nil); rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("fresh session re-set its cookie: %s", rec.Header().Get("Set-Cookie"))
	}

	// A session idle for two days slides back to a full TTL.
	if err := q.ExtendSession(ctx, db.ExtendSessionParams{TokenHash: hashToken(c.cookie.Value), UserID: me.Id, ExpiresAt: time.Now().Add(sessionTTL - 48*time.Hour)}); err != nil {
		t.Fatal(err)
	}
	rec := c.do(http.MethodGet, "/api/me", nil, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Set-Cookie") == "" {
		t.Fatalf("idle session not extended: %d %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
	row, err := q.GetSession(ctx, hashToken(c.cookie.Value))
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(row.ExpiresAt); d < sessionTTL-time.Minute {
		t.Fatalf("session expires in %v, want about %v", d, sessionTTL)
	}
}

func TestPasswordsNeverLogged(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	c, _ := newAPI(t, true, &logs)

	c.session(c.do(http.MethodPost, "/api/auth/register", register("alice"), nil), http.StatusCreated)
	c.do(http.MethodPost, "/api/auth/register", register("alice"), nil)
	c.do(http.MethodPost, "/api/auth/login", LoginRequest{Username: "alice", Password: testPassword + "-wrong"}, nil)
	c.do(http.MethodPost, "/api/auth/login", LoginRequest{Username: "alice", Password: testPassword}, nil)
	c.do(http.MethodPost, "/api/auth/register", `{"username":"bob","password":"`+testPassword, nil) // malformed JSON

	if logs.Len() == 0 {
		t.Fatal("no logs captured")
	}
	if strings.Contains(logs.String(), testPassword) {
		t.Fatalf("password leaked into logs: %s", logs.String())
	}
}

func TestPublicOperations(t *testing.T) {
	t.Parallel()

	// Every strict operation is either public or guarded; guarded ones without a
	// session must never reach the handler.
	reached := false
	next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
		reached = true
		return nil, nil
	}
	for _, op := range []string{"Logout", "GetMe", "UpdateMe"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		if _, err := requireSession(next, op)(req.Context(), httptest.NewRecorder(), req, nil); err == nil || reached {
			t.Fatalf("%s reachable without a session", op)
		}
	}
	for op := range publicOperations() {
		reached = false
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
		if _, err := requireSession(next, op)(req.Context(), httptest.NewRecorder(), req, nil); err != nil || !reached {
			t.Fatalf("public %s blocked: %v", op, err)
		}
	}
}

func TestGetConfig(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	if got := decodeJSON[Config](t, c.do(http.MethodGet, "/api/config", nil, nil), http.StatusOK); got.MapTileUrl != testTileURL {
		t.Errorf("config = %+v", got)
	}
	anon := &apiClient{t: t, h: c.h}
	wantProblem(t, anon.do(http.MethodGet, "/api/config", nil, nil), KindUnauthorized)
}
