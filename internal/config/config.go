// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config reads the hub's settings from the environment.
// Every variable is prefixed AICM_. Later tasks add their own fields here.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// masterKeySize is the length of the key that wraps org data keys.
const masterKeySize = 32

// Hub is the hub's runtime configuration.
type Hub struct {
	// DatabaseURL is a Postgres connection string (AICM_DATABASE_URL, required).
	DatabaseURL string
	// ListenAddr is the HTTP listen address (AICM_LISTEN_ADDR, default ":8080").
	ListenAddr string
	// PublicURL is the externally reachable base URL, used in enrollment
	// commands and email links (AICM_PUBLIC_URL, default "http://localhost:8080").
	PublicURL string
	// LogLevel is debug, info, warn or error (AICM_LOG_LEVEL, default "info").
	LogLevel slog.Level
	// LogFormat is json or text (AICM_LOG_FORMAT, default "json").
	LogFormat string
	// MasterKey is the 32-byte key that wraps every org data key.
	MasterKey []byte
	// OIDC is single sign-on; disabled unless issuer, client id and secret
	// are all set.
	OIDC OIDC
}

// OIDC holds the provider settings for single sign-on.
type OIDC struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Name         string
	Scopes       []string
}

// LoadHub reads the hub configuration from the environment.
func LoadHub() (Hub, error) {
	c := Hub{
		DatabaseURL: os.Getenv("AICM_DATABASE_URL"),
		ListenAddr:  getenv("AICM_LISTEN_ADDR", ":8080"),
		PublicURL:   strings.TrimRight(getenv("AICM_PUBLIC_URL", "http://localhost:8080"), "/"),
		LogFormat:   getenv("AICM_LOG_FORMAT", "json"),
		OIDC: OIDC{
			Issuer:       strings.TrimSpace(os.Getenv("AICM_OIDC_ISSUER")),
			ClientID:     strings.TrimSpace(os.Getenv("AICM_OIDC_CLIENT_ID")),
			ClientSecret: os.Getenv("AICM_OIDC_CLIENT_SECRET"),
			Name:         getenv("AICM_OIDC_NAME", "Single sign-on"),
			Scopes:       strings.Fields(getenv("AICM_OIDC_SCOPES", "openid email profile")),
		},
	}
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("AICM_DATABASE_URL is required"))
	}
	if u, err := url.Parse(c.PublicURL); err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("AICM_PUBLIC_URL %q is not an absolute URL", c.PublicURL))
	}
	if err := c.LogLevel.UnmarshalText([]byte(getenv("AICM_LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("AICM_LOG_LEVEL: %w", err))
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		errs = append(errs, fmt.Errorf("AICM_LOG_FORMAT %q must be json or text", c.LogFormat))
	}
	masterKey, err := LoadMasterKey()
	if err != nil {
		errs = append(errs, err)
	} else {
		c.MasterKey = masterKey
	}
	return c, errors.Join(errs...)
}

// LoadMasterKey reads the key that wraps org data keys, in order:
// AICM_MASTER_KEY, then AICM_MASTER_KEY_FILE. When the file is missing and
// AICM_MASTER_KEY_GENERATE=true it creates one (mode 0600) and uses it.
// Nothing here logs the key.
func LoadMasterKey() ([]byte, error) {
	if v := strings.TrimSpace(os.Getenv("AICM_MASTER_KEY")); v != "" {
		return decodeMasterKey("AICM_MASTER_KEY", v)
	}
	path := os.Getenv("AICM_MASTER_KEY_FILE")
	if path == "" {
		return nil, errors.New("AICM_MASTER_KEY or AICM_MASTER_KEY_FILE is required")
	}
	data, err := os.ReadFile(path) //nolint:gosec // path comes from AICM_MASTER_KEY_FILE
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("reading AICM_MASTER_KEY_FILE: %w", err)
		}
		if os.Getenv("AICM_MASTER_KEY_GENERATE") != "true" {
			return nil, errors.New("AICM_MASTER_KEY or AICM_MASTER_KEY_FILE is required")
		}
		return generateMasterKey(path)
	}
	return decodeMasterKey("AICM_MASTER_KEY_FILE", string(data))
}

// generateMasterKey creates a fresh key file and returns the key.
func generateMasterKey(path string) ([]byte, error) {
	key := make([]byte, masterKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating master key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { //nolint:gosec // path comes from AICM_MASTER_KEY_FILE
		return nil, fmt.Errorf("creating master key directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // path comes from AICM_MASTER_KEY_FILE
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			// Another process created the file first; use its key.
			data, err := os.ReadFile(path) //nolint:gosec // path comes from AICM_MASTER_KEY_FILE
			if err != nil {
				return nil, fmt.Errorf("reading AICM_MASTER_KEY_FILE: %w", err)
			}
			return decodeMasterKey("AICM_MASTER_KEY_FILE", string(data))
		}
		return nil, fmt.Errorf("creating master key file: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(key) + "\n"
	if _, err := f.WriteString(encoded); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("writing master key file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("closing master key file: %w", err)
	}
	return key, nil
}

// decodeMasterKey decodes base64 and requires exactly 32 bytes.
func decodeMasterKey(name, value string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("%s is not valid base64: %w", name, err)
	}
	if len(key) != masterKeySize {
		return nil, fmt.Errorf("%s must decode to %d bytes, got %d", name, masterKeySize, len(key))
	}
	return key, nil
}

// NewLogger builds the process logger from the configuration.
func (c Hub) NewLogger() *slog.Logger {
	opts := &slog.HandlerOptions{Level: c.LogLevel}
	if c.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
