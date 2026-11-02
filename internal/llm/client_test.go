package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iskyd/kudos/internal/secret"
)

// fake serves one canned response and records the request it got.
type fake struct {
	status int
	body   string
	got    *http.Request
	sent   map[string]any
}

func (f *fake) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.got = r
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &f.sent); err != nil {
			t.Errorf("request body: %v", err)
		}
		if strings.Contains(f.body, "data:") {
			w.Header().Set("Content-Type", "text/event-stream")
		}
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, f.body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func client(t *testing.T, p Provider, f *fake, key string) Client {
	t.Helper()
	srv := f.serve(t)
	return New(Config{Provider: p, BaseURL: srv.URL + "/", Model: "test-model", APIKey: secret.New(key)}, srv.Client())
}

var conversation = Request{
	System:   "You are a coach.",
	Messages: []Message{{Role: "user", Content: "How was my week?"}},
}

func collect(t *testing.T, c Client) ([]string, error) {
	t.Helper()
	var parts []string
	err := c.Stream(t.Context(), conversation, func(s string) error {
		parts = append(parts, s)
		return nil
	})
	return parts, err
}

func TestOpenAIComplete(t *testing.T) {
	t.Parallel()
	f := &fake{status: 200, body: `{"choices":[{"message":{"role":"assistant","content":"A solid week."}}]}`}
	c := client(t, OpenAI, f, "sk-test")
	got, err := c.Complete(t.Context(), conversation)
	if err != nil || got != "A solid week." {
		t.Fatalf("Complete = %q, %v", got, err)
	}
	if f.got.URL.Path != "/chat/completions" || f.got.Header.Get("Authorization") != "Bearer sk-test" {
		t.Errorf("request %s, auth %q", f.got.URL.Path, f.got.Header.Get("Authorization"))
	}
	msgs, _ := json.Marshal(f.sent["messages"])
	if f.sent["model"] != "test-model" || f.sent["max_tokens"] != float64(defaultMaxTokens) || f.sent["stream"] != nil ||
		string(msgs) != `[{"content":"You are a coach.","role":"system"},{"content":"How was my week?","role":"user"}]` {
		t.Errorf("body = %v", f.sent)
	}
}

func TestOllamaSendsNoKey(t *testing.T) {
	t.Parallel()
	f := &fake{status: 200, body: `{"choices":[{"message":{"content":"ok"}}]}`}
	c := client(t, Ollama, f, "")
	req := conversation
	req.System, req.MaxTokens = "", 100
	if _, err := c.Complete(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if h := f.got.Header.Get("Authorization"); h != "" {
		t.Errorf("Authorization = %q", h)
	}
	if msgs, _ := f.sent["messages"].([]any); f.sent["max_tokens"] != float64(100) || len(msgs) != 1 {
		t.Errorf("body = %v", f.sent)
	}
}

func TestOpenAIStream(t *testing.T) {
	t.Parallel()
	f := &fake{status: 200, body: "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"A solid\"}}]}\n\n" +
		": keep-alive\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" week.\"}}]}\n\n" +
		"data: [DONE]\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" ignored\"}}]}\n\n"}
	parts, err := collect(t, client(t, Gemini, f, "g-key"))
	if err != nil || strings.Join(parts, "|") != "A solid| week." {
		t.Fatalf("parts %q, %v", parts, err)
	}
	if f.sent["stream"] != true {
		t.Errorf("body = %v", f.sent)
	}
}

func TestAnthropicComplete(t *testing.T) {
	t.Parallel()
	f := &fake{status: 200, body: `{"content":[{"type":"text","text":"A solid"},{"type":"tool_use"},{"type":"text","text":" week."}],"stop_reason":"end_turn"}`}
	c := client(t, Anthropic, f, "ak-test")
	got, err := c.Complete(t.Context(), conversation)
	if err != nil || got != "A solid week." {
		t.Fatalf("Complete = %q, %v", got, err)
	}
	if f.got.URL.Path != "/v1/messages" || f.got.Header.Get("x-api-key") != "ak-test" ||
		f.got.Header.Get("anthropic-version") != anthropicVersion || f.got.Header.Get("Authorization") != "" {
		t.Errorf("request %s, headers %v", f.got.URL.Path, f.got.Header)
	}
	msgs, _ := json.Marshal(f.sent["messages"])
	if f.sent["system"] != "You are a coach." || string(msgs) != `[{"content":"How was my week?","role":"user"}]` || f.sent["model"] != "test-model" {
		t.Errorf("body = %v", f.sent)
	}
}

func TestAnthropicStream(t *testing.T) {
	t.Parallel()
	f := &fake{status: 200, body: "event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\"}\n\n" +
		"event: ping\ndata: {\"type\":\"ping\"}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"A solid\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\" week.\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\"}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"}
	parts, err := collect(t, client(t, Anthropic, f, "ak"))
	if err != nil || strings.Join(parts, "|") != "A solid| week." {
		t.Fatalf("parts %q, %v", parts, err)
	}
	if f.sent["stream"] != true {
		t.Errorf("body = %v", f.sent)
	}
}

func TestStreamErrors(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		provider Provider
		body     string
		want     string
		parts    int
	}{
		"openai error chunk":        {OpenAI, "data: {\"choices\":[{\"delta\":{\"content\":\"A\"}}]}\n\ndata: {\"error\":{\"message\":\"overloaded\"}}\n\n", "LLM provider error: overloaded", 1},
		"openai malformed":          {Perplexity, "data: {not json\n\n", "malformed", 0},
		"anthropic error":           {Anthropic, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n", "LLM provider error: Overloaded", 0},
		"anthropic malformed delta": {Anthropic, "event: content_block_delta\ndata: nope\n\n", "malformed", 0},
		"anthropic malformed error": {Anthropic, "event: error\ndata: nope\n\n", "malformed", 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parts, err := collect(t, client(t, tc.provider, &fake{status: 200, body: tc.body}, "k"))
			if err == nil || !strings.Contains(err.Error(), tc.want) || len(parts) != tc.parts {
				t.Errorf("parts %q, err %v; want %q", parts, err, tc.want)
			}
		})
	}
}

func TestStreamStopsWhenTheCallbackFails(t *testing.T) {
	t.Parallel()
	f := &fake{status: 200, body: "data: {\"choices\":[{\"delta\":{\"content\":\"A\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"B\"}}]}\n\n"}
	stop := errors.New("client went away")
	calls := 0
	err := client(t, OpenAI, f, "k").Stream(t.Context(), conversation, func(string) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || calls != 1 {
		t.Errorf("err %v after %d calls", err, calls)
	}
}

func TestProviderErrors(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 1000)
	for name, tc := range map[string]struct {
		provider Provider
		status   int
		body     string
		message  string
	}{
		"anthropic 401":     {Anthropic, 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`, "invalid x-api-key"},
		"openai 429":        {OpenAI, 429, `{"error":{"message":"Rate limit reached","type":"requests"}}`, "Rate limit reached"},
		"ollama 404":        {Ollama, 404, `{"error":"model \"llama\" not found"}`, `model "llama" not found`},
		"plain text 502":    {Gemini, 502, "Bad Gateway\n", "Bad Gateway"},
		"long body":         {OpenAI, 500, long, long[:maxErrorMessage] + "…"},
		"error of no shape": {Anthropic, 500, `{"error":42}`, `{"error":42}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := client(t, tc.provider, &fake{status: tc.status, body: tc.body}, "k")
			_, err := c.Complete(t.Context(), conversation)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Status != tc.status || apiErr.Message != tc.message {
				t.Fatalf("Complete error = %#v", err)
			}
			if want := fmt.Sprintf("LLM provider returned %d: ", tc.status); !strings.HasPrefix(err.Error(), want) {
				t.Errorf("message %q", err)
			}
			if _, err := collect(t, c); !errors.As(err, &apiErr) || apiErr.Status != tc.status {
				t.Errorf("Stream error = %v", err)
			}
		})
	}
}

func TestMalformedReplies(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		provider Provider
		body     string
	}{
		"openai not JSON":    {OpenAI, "<html>"},
		"openai no choices":  {OpenAI, `{"choices":[]}`},
		"anthropic not JSON": {Anthropic, "<html>"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := client(t, tc.provider, &fake{status: 200, body: tc.body}, "k").Complete(t.Context(), conversation); !errors.Is(err, ErrMalformed) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestUnreachableProvider(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for _, p := range []Provider{OpenAI, Anthropic} {
		c := New(Config{Provider: p, BaseURL: srv.URL, Model: "m"}, http.DefaultClient)
		if _, err := c.Complete(ctx, conversation); err == nil || !strings.Contains(err.Error(), "calling the LLM provider") {
			t.Errorf("%s Complete: %v", p, err)
		}
		if err := c.Stream(ctx, conversation, func(string) error { return nil }); err == nil {
			t.Errorf("%s Stream succeeded", p)
		}
	}
	// A base URL that is not a URL fails before any request.
	if _, err := New(Config{Provider: OpenAI, BaseURL: "http://bad host"}, http.DefaultClient).Complete(ctx, conversation); err == nil {
		t.Error("bad base URL accepted")
	}
}

func TestDefaultBaseURL(t *testing.T) {
	t.Parallel()
	for p, want := range map[Provider]string{
		Ollama:     "http://localhost:11434/v1",
		OpenAI:     "https://api.openai.com/v1",
		Gemini:     "https://generativelanguage.googleapis.com/v1beta/openai",
		Perplexity: "https://api.perplexity.ai",
		Anthropic:  "https://api.anthropic.com",
	} {
		if got := DefaultBaseURL(p); got != want {
			t.Errorf("%s: %s", p, got)
		}
	}
}

func TestReadSSE(t *testing.T) {
	t.Parallel()
	type ev struct{ event, data string }
	var got []ev
	err := readSSE(strings.NewReader("event: a\ndata: line one\ndata: line two\n\n\n: comment\ndata:no space\n\ndata: last without blank line"),
		func(event, data string) error {
			got = append(got, ev{event, data})
			return nil
		})
	want := []ev{{"a", "line one\nline two"}, {"", "no space"}, {"", "last without blank line"}}
	if err != nil || len(got) != len(want) {
		t.Fatalf("got %v, %v", got, err)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %v, want %v", i, got[i], want[i])
		}
	}
	boom := errors.New("boom")
	if err := readSSE(strings.NewReader("data: x"), func(string, string) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("trailing event error = %v", err)
	}
	if err := readSSE(strings.NewReader("data: x"), func(string, string) error { return errStop }); err != nil {
		t.Errorf("trailing stop = %v", err)
	}
	if err := readSSE(strings.NewReader("data: "+strings.Repeat("x", 2<<20)+"\n\n"), func(string, string) error { return nil }); err == nil {
		t.Error("oversized line accepted")
	}
}
