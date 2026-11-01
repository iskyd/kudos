package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/iskyd/kudos/internal/secret"
)

// Provider is a supported LLM service.
type Provider string

// The providers: Anthropic has its own client, the rest speak the OpenAI API.
const (
	Ollama     Provider = "ollama"
	OpenAI     Provider = "openai"
	Gemini     Provider = "gemini"
	Perplexity Provider = "perplexity"
	Anthropic  Provider = "anthropic"
)

// DefaultBaseURL is where the provider's API lives unless the user says otherwise.
func DefaultBaseURL(p Provider) string {
	switch p {
	case Ollama:
		return "http://localhost:11434/v1"
	case OpenAI:
		return "https://api.openai.com/v1"
	case Gemini:
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case Perplexity:
		return "https://api.perplexity.ai"
	default:
		return "https://api.anthropic.com"
	}
}

// Config is a user's provider settings.
type Config struct {
	Provider Provider
	BaseURL  string
	Model    string
	APIKey   secret.Value[string] // empty for providers without a key (Ollama)
}

// Message is one turn of a conversation.
type Message struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// Request is a conversation for the model to continue.
type Request struct {
	System    string
	Messages  []Message
	MaxTokens int // 0 for defaultMaxTokens
}

const defaultMaxTokens = 4096

func (r Request) maxTokens() int {
	if r.MaxTokens > 0 {
		return r.MaxTokens
	}
	return defaultMaxTokens
}

// Client talks to one provider.
type Client interface {
	// Complete returns the model's whole reply.
	Complete(ctx context.Context, req Request) (string, error)
	// Stream calls onText with each piece of the reply as it arrives.
	Stream(ctx context.Context, req Request, onText func(string) error) error
}

// New returns the client for cfg's provider, sending requests with hc.
func New(cfg Config, hc *http.Client) Client {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Provider == Anthropic {
		return &anthropicClient{cfg: cfg, hc: hc}
	}
	return &openAIClient{cfg: cfg, hc: hc}
}

// APIError is a provider's refusal of a request.
type APIError struct {
	Status  int    // HTTP status; 0 for an error reported inside a stream
	Message string // the provider's message, truncated
}

func (e *APIError) Error() string {
	if e.Status == 0 {
		return "LLM provider error: " + e.Message
	}
	return fmt.Sprintf("LLM provider returned %d: %s", e.Status, e.Message)
}

// ErrMalformed means the provider's reply could not be read.
var ErrMalformed = errors.New("malformed LLM provider response")

// maxErrorMessage bounds how much of a provider's error is kept.
const maxErrorMessage = 300

// post sends body as JSON and returns the response, or an APIError for a non-2xx status.
func post(ctx context.Context, hc *http.Client, url string, headers map[string]string, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding the LLM request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("building the LLM request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling the LLM provider: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		return nil, &APIError{Status: resp.StatusCode, Message: errorMessage(resp.Body)}
	}
	return resp, nil
}

// errorMessage reads a provider error body: {"error": {"message": ...}} (both
// APIs), {"error": "..."} (Ollama) or plain text.
func errorMessage(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 8<<10)) // the status alone still reports the error
	var body struct {
		Error json.RawMessage `json:"error"`
	}
	msg := strings.TrimSpace(string(b))
	if json.Unmarshal(b, &body) == nil && body.Error != nil {
		var nested struct {
			Message string `json:"message"`
		}
		var flat string
		switch {
		case json.Unmarshal(body.Error, &nested) == nil && nested.Message != "":
			msg = nested.Message
		case json.Unmarshal(body.Error, &flat) == nil:
			msg = flat
		}
	}
	if len(msg) > maxErrorMessage {
		msg = msg[:maxErrorMessage] + "…"
	}
	return msg
}

// readSSE calls onEvent with each server-sent event's type and data until the
// stream ends or onEvent returns errStop or an error.
func readSSE(r io.Reader, onEvent func(event, data string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var event string
	var data []string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if len(data) > 0 {
				if err := onEvent(event, strings.Join(data, "\n")); err != nil {
					if errors.Is(err, errStop) {
						return nil
					}
					return err
				}
			}
			event, data = "", nil
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("reading the LLM stream: %w", err)
	}
	if len(data) > 0 {
		if err := onEvent(event, strings.Join(data, "\n")); err != nil && !errors.Is(err, errStop) {
			return err
		}
	}
	return nil
}

// errStop ends a stream early without an error.
var errStop = errors.New("end of stream")
