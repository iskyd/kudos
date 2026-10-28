package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	rawKey   = bytes.Repeat([]byte{7}, 32)
	validKey = base64.StdEncoding.EncodeToString(rawKey)
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func base() map[string]string {
	return map[string]string{
		"DATABASE_URL":   "postgres://kudos:pw@db/kudos",
		"ENCRYPTION_KEY": validKey,
	}
}

func with(kv ...string) map[string]string {
	m := base()
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := Load(env(base()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL.Reveal() != "postgres://kudos:pw@db/kudos" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL.Reveal())
	}
	if !bytes.Equal(cfg.EncryptionKey.Reveal(), rawKey) {
		t.Errorf("EncryptionKey not decoded")
	}
	if !cfg.RegistrationEnabled {
		t.Errorf("RegistrationEnabled = false, want true")
	}
	if cfg.ListenAddr != DefaultListenAddr || cfg.DataDir != DefaultDataDir || cfg.MapTileURL != DefaultMapTileURL {
		t.Errorf("defaults = %q %q %q", cfg.ListenAddr, cfg.DataDir, cfg.MapTileURL)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Parallel()

	keyFile := writeFile(t, validKey+"\n")
	m := with(
		"ENCRYPTION_KEY", "",
		"ENCRYPTION_KEY_FILE", keyFile,
		"REGISTRATION_ENABLED", "false",
		"LISTEN_ADDR", "127.0.0.1:9000",
		"DATA_DIR", "/srv/kudos",
		"MAP_TILE_URL", "http://tiles.lan/{z}/{x}/{y}.png?key=abc",
		"LOG_LEVEL", "DEBUG",
	)

	cfg, err := Load(env(m))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(cfg.EncryptionKey.Reveal(), rawKey) {
		t.Errorf("key from file not decoded")
	}
	if cfg.RegistrationEnabled {
		t.Errorf("RegistrationEnabled = true, want false")
	}
	if cfg.ListenAddr != "127.0.0.1:9000" || cfg.DataDir != "/srv/kudos" || cfg.MapTileURL != m["MAP_TILE_URL"] {
		t.Errorf("overrides = %q %q %q", cfg.ListenAddr, cfg.DataDir, cfg.MapTileURL)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", cfg.LogLevel)
	}
}

func TestLoadInvalid(t *testing.T) {
	t.Parallel()

	shortKey := base64.StdEncoding.EncodeToString(rawKey[:16])
	shortKeyFile := writeFile(t, shortKey)

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "missing database url", env: with("DATABASE_URL", ""), wantErr: "DATABASE_URL is required"},
		{name: "missing key", env: with("ENCRYPTION_KEY", ""), wantErr: "ENCRYPTION_KEY or ENCRYPTION_KEY_FILE is required"},
		{name: "both keys", env: with("ENCRYPTION_KEY_FILE", "/run/secrets/key"), wantErr: "set only one of"},
		{name: "key not base64", env: with("ENCRYPTION_KEY", "not base64!"), wantErr: "ENCRYPTION_KEY must be 32 random bytes"},
		{name: "key too short", env: with("ENCRYPTION_KEY", shortKey), wantErr: "ENCRYPTION_KEY must be 32 random bytes"},
		{name: "key file missing", env: with("ENCRYPTION_KEY", "", "ENCRYPTION_KEY_FILE", "/nonexistent/key"), wantErr: "reading ENCRYPTION_KEY_FILE"},
		{name: "key file too short", env: with("ENCRYPTION_KEY", "", "ENCRYPTION_KEY_FILE", shortKeyFile), wantErr: "ENCRYPTION_KEY_FILE must be 32 random bytes"},
		{name: "bad registration flag", env: with("REGISTRATION_ENABLED", "maybe"), wantErr: "REGISTRATION_ENABLED must be true or false"},
		{name: "tile url not http", env: with("MAP_TILE_URL", "ftp://tiles/{z}/{x}/{y}.png"), wantErr: "MAP_TILE_URL must be an http(s) URL"},
		{name: "tile url no host", env: with("MAP_TILE_URL", "https:///{z}/{x}/{y}.png"), wantErr: "MAP_TILE_URL must be an http(s) URL"},
		{name: "tile url unparsable", env: with("MAP_TILE_URL", "://bad"), wantErr: "MAP_TILE_URL must be an http(s) URL"},
		{name: "tile url missing placeholder", env: with("MAP_TILE_URL", "https://tiles/{z}/{x}.png"), wantErr: "MAP_TILE_URL must contain {y}"},
		{name: "bad log level", env: with("LOG_LEVEL", "verbose"), wantErr: "LOG_LEVEL must be debug, info, warn or error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Load(env(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	t.Parallel()

	_, err := Load(env(map[string]string{"LOG_LEVEL": "loud"}))
	if err == nil {
		t.Fatal("Load() error = nil")
	}
	for _, want := range []string{"DATABASE_URL", "ENCRYPTION_KEY", "LOG_LEVEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestConfigDoesNotLeakSecrets(t *testing.T) {
	t.Parallel()

	cfg, err := Load(env(base()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "cfg", cfg)
	out := buf.String() + fmt.Sprintf("%+v %#v", cfg, cfg)

	for _, leak := range []string{"pw@db", validKey} {
		if strings.Contains(out, leak) {
			t.Fatalf("secret %q leaked: %s", leak, out)
		}
	}
}

func TestKeyErrorsDoNotLeakKey(t *testing.T) {
	t.Parallel()

	bad := strings.Repeat("A", 20) + "!"
	_, err := Load(env(with("ENCRYPTION_KEY", bad)))
	if err == nil || strings.Contains(err.Error(), bad) {
		t.Fatalf("error leaked key material: %v", err)
	}
}
