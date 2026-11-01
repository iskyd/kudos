-- name: PutLLMProvider :one
-- Sets the user's provider. With replace_key the key is key_ciphertext (null
-- removes it); without, the stored key is kept, but only for the same provider,
-- so one provider's key is never sent to another.
INSERT INTO llm_providers (user_id, provider, base_url, model, key_ciphertext)
VALUES (@user_id, @provider, @base_url, @model, sqlc.narg(key_ciphertext))
ON CONFLICT (user_id) DO UPDATE SET
    provider = EXCLUDED.provider, base_url = EXCLUDED.base_url, model = EXCLUDED.model,
    key_ciphertext = CASE
        WHEN @replace_key::boolean THEN EXCLUDED.key_ciphertext
        WHEN llm_providers.provider = EXCLUDED.provider THEN llm_providers.key_ciphertext
    END,
    updated_at = now()
RETURNING *;

-- name: GetLLMProvider :one
SELECT * FROM llm_providers WHERE user_id = @user_id;

-- name: DeleteLLMProvider :execrows
DELETE FROM llm_providers WHERE user_id = @user_id;
