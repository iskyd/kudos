package store_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/iskyd/kudos/internal/store/db"
	"github.com/iskyd/kudos/internal/testutil"
)

func TestLLMProviderKeyRules(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, _ := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()
	put := func(p db.PutLLMProviderParams) db.LlmProvider {
		t.Helper()
		p.UserID = alice.ID
		got, err := q.PutLLMProvider(ctx, p)
		check(t, err)
		return got
	}
	sealed := []byte("ciphertext")

	got := put(db.PutLLMProviderParams{Provider: "anthropic", BaseUrl: "https://llm.example.test", Model: "m1", KeyCiphertext: sealed, ReplaceKey: true})
	if !bytes.Equal(got.KeyCiphertext, sealed) {
		t.Fatalf("created: %+v", got)
	}
	// Same provider without a new key keeps the stored one.
	got = put(db.PutLLMProviderParams{Provider: "anthropic", BaseUrl: "https://llm.example.test", Model: "m2"})
	if got.Model != "m2" || !bytes.Equal(got.KeyCiphertext, sealed) {
		t.Errorf("model change: %+v", got)
	}
	// Another provider never inherits it.
	got = put(db.PutLLMProviderParams{Provider: "openai", BaseUrl: "https://llm.example.test/v1", Model: "m3"})
	if got.KeyCiphertext != nil {
		t.Errorf("switched provider kept the key: %+v", got)
	}
	// Replacing with nothing removes a key.
	put(db.PutLLMProviderParams{Provider: "openai", BaseUrl: "https://llm.example.test/v1", Model: "m3", KeyCiphertext: sealed, ReplaceKey: true})
	if got = put(db.PutLLMProviderParams{Provider: "openai", BaseUrl: "https://llm.example.test/v1", Model: "m3", ReplaceKey: true}); got.KeyCiphertext != nil {
		t.Errorf("removed key kept: %+v", got)
	}

	for name, p := range map[string]db.PutLLMProviderParams{
		"unknown provider": {UserID: alice.ID, Provider: "acme", BaseUrl: "https://llm.example.test", Model: "m"},
		"empty base URL":   {UserID: alice.ID, Provider: "ollama", BaseUrl: "", Model: "m"},
		"empty model":      {UserID: alice.ID, Provider: "ollama", BaseUrl: "http://ollama.example.test", Model: ""},
	} {
		if _, err := q.PutLLMProvider(ctx, p); err == nil {
			t.Errorf("%s: stored", name)
		}
	}
}

func TestLLMProviderIsolation(t *testing.T) {
	t.Parallel()
	pool := pg.NewDB(t)
	alice, bob := testutil.TwoUsers(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	_, err := q.PutLLMProvider(ctx, db.PutLLMProviderParams{
		UserID: alice.ID, Provider: "anthropic", BaseUrl: "https://llm.example.test", Model: "m", KeyCiphertext: []byte("k"), ReplaceKey: true,
	})
	check(t, err)
	if _, err := q.GetLLMProvider(ctx, bob.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("bob reads alice's provider: %v", err)
	}
	if n, err := q.DeleteLLMProvider(ctx, bob.ID); err != nil || n != 0 {
		t.Errorf("bob deletes %d rows, %v", n, err)
	}
	// Bob's own provider is a separate row.
	_, err = q.PutLLMProvider(ctx, db.PutLLMProviderParams{UserID: bob.ID, Provider: "ollama", BaseUrl: "http://ollama.example.test", Model: "m"})
	check(t, err)
	if got, err := q.GetLLMProvider(ctx, alice.ID); err != nil || got.Provider != "anthropic" || string(got.KeyCiphertext) != "k" {
		t.Errorf("alice's provider changed: %+v, %v", got, err)
	}
	if n, err := q.DeleteLLMProvider(ctx, alice.ID); err != nil || n != 1 {
		t.Errorf("alice deletes %d rows, %v", n, err)
	}
	if _, err := q.GetLLMProvider(ctx, bob.ID); err != nil {
		t.Errorf("bob's provider gone: %v", err)
	}
}
