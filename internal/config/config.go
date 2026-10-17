// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config reads the hub's settings from the environment.
// Every variable is prefixed AICM_. Later tasks add their own fields here.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

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
}

// LoadHub reads the hub configuration from the environment.
func LoadHub() (Hub, error) {
	c := Hub{
		DatabaseURL: os.Getenv("AICM_DATABASE_URL"),
		ListenAddr:  getenv("AICM_LISTEN_ADDR", ":8080"),
		PublicURL:   strings.TrimRight(getenv("AICM_PUBLIC_URL", "http://localhost:8080"), "/"),
		LogFormat:   getenv("AICM_LOG_FORMAT", "json"),
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
	return c, errors.Join(errs...)
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
