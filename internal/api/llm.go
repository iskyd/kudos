package api

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/llm"
	"github.com/iskyd/kudos/internal/store/db"
)

// Keep in step with LLMProviderInput in api/openapi.yaml.
const (
	maxLLMBaseURL = 500
	maxLLMModel   = 200
	maxLLMKey     = 500
)

func llmProvider(p db.LlmProvider) LLMProvider {
	kind := LLMProviderKind(p.Provider)
	return LLMProvider{Configured: true, Provider: &kind, BaseUrl: &p.BaseUrl, Model: &p.Model, KeySet: p.KeyCiphertext != nil}
}

// GetLLMProvider returns the provider settings, never the key.
func (s *Server) GetLLMProvider(ctx context.Context, _ GetLLMProviderRequestObject) (GetLLMProviderResponseObject, error) {
	p, err := s.q.GetLLMProvider(ctx, sessionFrom(ctx).userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GetLLMProvider200JSONResponse{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the LLM provider: %w", err)
	}
	return GetLLMProvider200JSONResponse(llmProvider(p)), nil
}

// SetLLMProvider replaces the provider, storing a new key encrypted.
func (s *Server) SetLLMProvider(ctx context.Context, req SetLLMProviderRequestObject) (SetLLMProviderResponseObject, error) {
	b := req.Body
	userID := sessionFrom(ctx).userID
	invalid := func(detail string) (SetLLMProviderResponseObject, error) {
		return nil, &Error{Kind: KindInvalidRequest, Detail: detail}
	}
	if !b.Provider.Valid() {
		return invalid("unknown provider")
	}
	baseURL := llm.DefaultBaseURL(llm.Provider(b.Provider))
	if b.BaseUrl != nil && strings.TrimSpace(*b.BaseUrl) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(*b.BaseUrl), "/")
	}
	if u, err := url.Parse(baseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(baseURL) > maxLLMBaseURL {
		return invalid(fmt.Sprintf("baseUrl must be an http or https URL of at most %d characters", maxLLMBaseURL))
	}
	model := strings.TrimSpace(b.Model)
	if model == "" || utf8.RuneCountInString(model) > maxLLMModel {
		return invalid(fmt.Sprintf("model must be 1 to %d characters", maxLLMModel))
	}
	params := db.PutLLMProviderParams{UserID: userID, Provider: string(b.Provider), BaseUrl: baseURL, Model: model, ReplaceKey: b.ApiKey != nil}
	key := ""
	if b.ApiKey != nil {
		if key = strings.TrimSpace(*b.ApiKey); len(key) > maxLLMKey {
			return invalid(fmt.Sprintf("apiKey must be at most %d characters", maxLLMKey))
		}
	}
	if b.Provider != Ollama && key == "" {
		// Without a new key, the stored one counts only for the same provider.
		current, err := s.q.GetLLMProvider(ctx, userID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("reading the LLM provider: %w", err)
		}
		if b.ApiKey != nil || err != nil || current.Provider != string(b.Provider) || current.KeyCiphertext == nil {
			return invalid("apiKey is required for this provider")
		}
	}
	if key != "" {
		sealed, err := s.cipher.Seal([]byte(key))
		if err != nil {
			return nil, fmt.Errorf("encrypting the LLM key: %w", err)
		}
		params.KeyCiphertext = sealed
	}
	saved, err := s.q.PutLLMProvider(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("storing the LLM provider: %w", err)
	}
	return SetLLMProvider200JSONResponse(llmProvider(saved)), nil
}

// DeleteLLMProvider removes the provider and its key.
func (s *Server) DeleteLLMProvider(ctx context.Context, _ DeleteLLMProviderRequestObject) (DeleteLLMProviderResponseObject, error) {
	if _, err := s.q.DeleteLLMProvider(ctx, sessionFrom(ctx).userID); err != nil {
		return nil, fmt.Errorf("deleting the LLM provider: %w", err)
	}
	return DeleteLLMProvider204Response{}, nil
}
