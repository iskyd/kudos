// Package config loads and validates the server configuration from environment variables.
package config

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/iskyd/kudos/internal/crypto"
	"github.com/iskyd/kudos/internal/secret"
)

// Defaults for optional variables. Keep deploy/.env.example in sync.
const (
	DefaultListenAddr = ":8080"
	DefaultDataDir    = "/data"
	DefaultMapTileURL = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"
	// DefaultGarminAPIURL is Garmin's Personal API host.
	DefaultGarminAPIURL = "https://personal.connectapi.garmin.com"
)

// Config is the validated server configuration.
type Config struct {
	DatabaseURL         secret.Value[string]
	EncryptionKey       secret.Value[[]byte]
	RegistrationEnabled bool
	ListenAddr          string
	DataDir             string
	MapTileURL          string
	GarminAPIURL        string
	LogLevel            slog.Level
}

// Load reads the configuration through getenv (os.Getenv in production) and
// returns every validation problem at once. Empty values count as unset.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		RegistrationEnabled: true,
		ListenAddr:          or(getenv("LISTEN_ADDR"), DefaultListenAddr),
		DataDir:             or(getenv("DATA_DIR"), DefaultDataDir),
		MapTileURL:          or(getenv("MAP_TILE_URL"), DefaultMapTileURL),
		GarminAPIURL:        or(getenv("GARMIN_API_URL"), DefaultGarminAPIURL),
		LogLevel:            slog.LevelInfo,
	}
	var errs []error

	if v := getenv("DATABASE_URL"); v == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	} else {
		cfg.DatabaseURL = secret.New(v)
	}

	key, err := loadKey(getenv("ENCRYPTION_KEY"), getenv("ENCRYPTION_KEY_FILE"))
	if err != nil {
		errs = append(errs, err)
	} else {
		cfg.EncryptionKey = secret.New(key)
	}

	if v := getenv("REGISTRATION_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("REGISTRATION_ENABLED must be true or false, got %q", v))
		}
		cfg.RegistrationEnabled = b
	}

	if err := validateTileURL(cfg.MapTileURL); err != nil {
		errs = append(errs, err)
	}

	if u, err := url.Parse(cfg.GarminAPIURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, errors.New("GARMIN_API_URL must be an http(s) URL"))
	}

	if v := getenv("LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error, got %q", v))
		}
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

// loadKey returns the decoded key from exactly one of the inline value or the
// file (a Docker secret). Errors never include key material.
func loadKey(inline, file string) ([]byte, error) {
	switch {
	case inline != "" && file != "":
		return nil, errors.New("set only one of ENCRYPTION_KEY and ENCRYPTION_KEY_FILE")
	case inline == "" && file == "":
		return nil, errors.New("ENCRYPTION_KEY or ENCRYPTION_KEY_FILE is required")
	}

	name, raw := "ENCRYPTION_KEY", []byte(inline)
	if file != "" {
		name = "ENCRYPTION_KEY_FILE"
		b, err := os.ReadFile(file) //nolint:gosec // path comes from the operator's environment
		if err != nil {
			return nil, fmt.Errorf("reading ENCRYPTION_KEY_FILE: %w", err)
		}
		raw = b
	}

	key, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(raw)))
	if err != nil || len(key) != crypto.KeySize {
		return nil, fmt.Errorf("%s must be %d random bytes, base64-encoded (openssl rand -base64 32)", name, crypto.KeySize)
	}
	return key, nil
}

// validateTileURL never echoes the URL: keyed providers embed an API key in it.
func validateTileURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("MAP_TILE_URL must be an http(s) URL")
	}
	for _, p := range []string{"{z}", "{x}", "{y}"} {
		if !strings.Contains(raw, p) {
			return fmt.Errorf("MAP_TILE_URL must contain %s", p)
		}
	}
	return nil
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
