package api

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

const llmKey = "sk-ant-test-0123456789"

func setLLM(t *testing.T, c *apiClient, in LLMProviderInput) LLMProvider {
	t.Helper()
	rec := c.do(http.MethodPut, "/api/llm-provider", in, nil)
	if strings.Contains(rec.Body.String(), llmKey) {
		t.Errorf("the response returns the key: %s", rec.Body)
	}
	return decodeJSON[LLMProvider](t, rec, http.StatusOK)
}

func getLLM(t *testing.T, c *apiClient) LLMProvider {
	t.Helper()
	rec := c.do(http.MethodGet, "/api/llm-provider", nil, nil)
	if strings.Contains(rec.Body.String(), llmKey) {
		t.Errorf("the response returns the key: %s", rec.Body)
	}
	return decodeJSON[LLMProvider](t, rec, http.StatusOK)
}

func TestLLMProviderKeyIsWriteOnly(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	c, q := newAPI(t, true, &logs)
	me := c.session(c.do(http.MethodPost, "/api/auth/register", register("alice"), nil), http.StatusCreated)

	if got := getLLM(t, c); got.Configured || got.KeySet || got.Provider != nil {
		t.Fatalf("before setup: %+v", got)
	}
	got := setLLM(t, c, LLMProviderInput{Provider: Anthropic, Model: " claude-test ", ApiKey: new(" " + llmKey + " ")})
	if !got.Configured || !got.KeySet || *got.Provider != Anthropic || *got.BaseUrl != "https://api.anthropic.com" || *got.Model != "claude-test" {
		t.Fatalf("saved: %+v", got)
	}
	if got := getLLM(t, c); !got.KeySet || *got.Model != "claude-test" {
		t.Errorf("read back: %+v", got)
	}

	// Stored as ciphertext that opens to the trimmed key.
	row, err := q.GetLLMProvider(t.Context(), me.Id)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(row.KeyCiphertext, []byte(llmKey)) {
		t.Fatal("the key is stored in plain text")
	}
	if plain, err := testCipher(t).Open(row.KeyCiphertext); err != nil || string(plain) != llmKey {
		t.Errorf("stored key opens to %q, %v", plain, err)
	}
	if strings.Contains(logs.String(), llmKey) {
		t.Error("logs contain the key")
	}
}

func TestLLMProviderKeyRulesAPI(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")

	// Ollama needs no key; the base URL loses its trailing slash.
	got := setLLM(t, c, LLMProviderInput{Provider: Ollama, BaseUrl: new("http://ollama.example.test:11434/v1/"), Model: "llama3"})
	if got.KeySet || *got.BaseUrl != "http://ollama.example.test:11434/v1" {
		t.Errorf("ollama: %+v", got)
	}
	// Every other provider does, and Ollama's missing key does not carry over.
	wantProblem(t, c.do(http.MethodPut, "/api/llm-provider", LLMProviderInput{Provider: Openai, Model: "gpt"}, nil), KindInvalidRequest)
	got = setLLM(t, c, LLMProviderInput{Provider: Openai, Model: "gpt", ApiKey: new(llmKey)})
	if !got.KeySet || *got.BaseUrl != "https://api.openai.com/v1" {
		t.Errorf("openai: %+v", got)
	}
	// Left out, the key stays for the same provider.
	if got = setLLM(t, c, LLMProviderInput{Provider: Openai, Model: "gpt-mini"}); !got.KeySet || *got.Model != "gpt-mini" {
		t.Errorf("model change: %+v", got)
	}
	// Another provider never gets it.
	wantProblem(t, c.do(http.MethodPut, "/api/llm-provider", LLMProviderInput{Provider: Gemini, Model: "gemini"}, nil), KindInvalidRequest)
	// An empty key cannot leave a keyed provider without one.
	wantProblem(t, c.do(http.MethodPut, "/api/llm-provider", LLMProviderInput{Provider: Openai, Model: "gpt", ApiKey: new(" ")}, nil), KindInvalidRequest)
	if got := getLLM(t, c); !got.KeySet || *got.Provider != Openai || *got.Model != "gpt-mini" {
		t.Errorf("a refused change was stored: %+v", got)
	}
	// Back to Ollama, an empty key removes the stored one.
	if got = setLLM(t, c, LLMProviderInput{Provider: Ollama, Model: "llama3", ApiKey: new("")}); got.KeySet {
		t.Errorf("ollama with an empty key: %+v", got)
	}

	if rec := c.do(http.MethodDelete, "/api/llm-provider", nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if got := getLLM(t, c); got.Configured {
		t.Errorf("after delete: %+v", got)
	}
	if rec := c.do(http.MethodDelete, "/api/llm-provider", nil, nil); rec.Code != http.StatusNoContent {
		t.Errorf("deleting nothing: %d", rec.Code)
	}
}

func TestLLMProviderValidation(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	for name, in := range map[string]LLMProviderInput{
		"unknown provider":  {Provider: "acme", Model: "m", ApiKey: new(llmKey)},
		"ftp base URL":      {Provider: Ollama, BaseUrl: new("ftp://ollama.example.test"), Model: "m"},
		"base URL no host":  {Provider: Ollama, BaseUrl: new("http://"), Model: "m"},
		"base URL too long": {Provider: Ollama, BaseUrl: new("http://example.test/" + strings.Repeat("a", maxLLMBaseURL)), Model: "m"},
		"empty model":       {Provider: Ollama, Model: "  "},
		"model too long":    {Provider: Ollama, Model: strings.Repeat("m", maxLLMModel+1)},
		"key too long":      {Provider: Openai, Model: "m", ApiKey: new(strings.Repeat("k", maxLLMKey+1))},
	} {
		t.Run(name, func(t *testing.T) {
			rec := c.do(http.MethodPut, "/api/llm-provider", in, nil)
			if in.ApiKey != nil && len(*in.ApiKey) > 10 && strings.Contains(rec.Body.String(), *in.ApiKey) {
				t.Error("the problem echoes the key")
			}
			wantProblem(t, rec, KindInvalidRequest)
		})
	}
	if got := getLLM(t, c); got.Configured {
		t.Errorf("an invalid provider was stored: %+v", got)
	}
}

func TestLLMProviderIsolationAPI(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	setLLM(t, c, LLMProviderInput{Provider: Anthropic, Model: "claude-test", ApiKey: new(llmKey)})

	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	if got := getLLM(t, bob); got.Configured {
		t.Errorf("bob reads alice's provider: %+v", got)
	}
	// Bob cannot keep a key he never set, even naming alice's provider.
	wantProblem(t, bob.do(http.MethodPut, "/api/llm-provider", LLMProviderInput{Provider: Anthropic, Model: "claude-test"}, nil), KindInvalidRequest)
	if rec := bob.do(http.MethodDelete, "/api/llm-provider", nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("bob delete: %d", rec.Code)
	}
	if got := getLLM(t, c); !got.Configured || !got.KeySet {
		t.Errorf("alice's provider changed: %+v", got)
	}
}
