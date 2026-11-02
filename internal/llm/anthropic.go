package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// anthropicVersion is the Messages API version the client speaks.
const anthropicVersion = "2023-06-01"

// anthropicClient speaks Anthropic's native Messages API.
type anthropicClient struct {
	cfg Config
	hc  *http.Client
}

type anthropicRequest struct {
	Model     string    `json:"model"`
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream,omitempty"`
}

func (c *anthropicClient) post(ctx context.Context, req Request, stream bool) (*http.Response, error) {
	body := anthropicRequest{Model: c.cfg.Model, System: req.System, Messages: req.Messages, MaxTokens: req.maxTokens(), Stream: stream}
	headers := map[string]string{"x-api-key": c.cfg.APIKey.Reveal(), "anthropic-version": anthropicVersion}
	return post(ctx, c.hc, c.cfg.BaseURL+"/v1/messages", headers, body)
}

func (c *anthropicClient) Complete(ctx context.Context, req Request) (string, error) {
	resp, err := c.post(ctx, req, false)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	var text strings.Builder
	for _, block := range out.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return text.String(), nil
}

func (c *anthropicClient) Stream(ctx context.Context, req Request, onText func(string) error) error {
	resp, err := c.post(ctx, req, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return readSSE(resp.Body, func(event, data string) error {
		switch event {
		case "message_stop":
			return errStop
		case "error":
			var e struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &e); err != nil {
				return fmt.Errorf("%w: %w", ErrMalformed, err)
			}
			return &APIError{Message: e.Error.Message}
		case "content_block_delta":
			var d struct {
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &d); err != nil {
				return fmt.Errorf("%w: %w", ErrMalformed, err)
			}
			if d.Delta.Type == "text_delta" && d.Delta.Text != "" {
				return onText(d.Delta.Text)
			}
		}
		return nil // message_start, ping and the rest carry no text
	})
}
