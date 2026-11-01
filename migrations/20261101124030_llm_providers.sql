-- +goose Up
-- The user's one active LLM provider. The API key is AES-256-GCM ciphertext;
-- the encryption key never touches the database. Ollama needs no key.
CREATE TABLE llm_providers (
    user_id        bigint      PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    provider       text        NOT NULL CHECK (provider IN ('ollama', 'openai', 'gemini', 'perplexity', 'anthropic')),
    base_url       text        NOT NULL CHECK (char_length(base_url) BETWEEN 1 AND 500),
    model          text        NOT NULL CHECK (char_length(model) BETWEEN 1 AND 200),
    key_ciphertext bytea,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE llm_providers;
