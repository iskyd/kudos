package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// openAIClient speaks the OpenAI chat completions API, which Ollama, Gemini and
// Perplexity also offer.
type openAIClient struct {
	cfg Config
	hc  *http.Client
}

type openAIRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream,omitempty"`
}

func (c *openAIClient) body(req Request, stream bool) openAIRequest {
	msgs := make([]Message, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, Message{Role: "system", Content: req.System})
	}
	msgs = append(msgs, req.Messages...)
	return openAIRequest{Model: c.cfg.Model, Messages: msgs, MaxTokens: req.maxTokens(), Stream: stream}
}

func (c *openAIClient) post(ctx context.Context, body openAIRequest) (*http.Response, error) {
	headers := map[string]string{}
	if key := c.cfg.APIKey.Reveal(); key != "" {
		headers["Authorization"] = "Bearer " + key
	}
	return post(ctx, c.hc, c.cfg.BaseURL+"/chat/completions", headers, body)
}

func (c *openAIClient) Complete(ctx context.Context, req Request) (string, error) {
	resp, err := c.post(ctx, c.body(req, false))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("%w: no choices", ErrMalformed)
	}
	return out.Choices[0].Message.Content, nil
}

func (c *openAIClient) Stream(ctx context.Context, req Request, onText func(string) error) error {
	resp, err := c.post(ctx, c.body(req, true))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return readSSE(resp.Body, func(_, data string) error {
		if data == "[DONE]" {
			return errStop
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("%w: %w", ErrMalformed, err)
		}
		if chunk.Error != nil {
			return &APIError{Message: chunk.Error.Message}
		}
		if len(chunk.Choices) == 0 || chunk.Choices[0].Delta.Content == "" {
			return nil
		}
		return onText(chunk.Choices[0].Delta.Content)
	})
}
