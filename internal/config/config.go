package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultInterval = 5 * time.Second
	minInterval     = 3 * time.Second
	maxInterval     = 30 * time.Second
)

// Config stores runtime settings loaded from environment variables.
type Config struct {
	BackendURL     string
	ServerID       string
	ReportInterval time.Duration
	AgentToken     string
}

// Load reads and validates environment variables, applying defaults when possible.
func Load() (Config, error) {
	cfg := Config{
		BackendURL: strings.TrimSpace(os.Getenv("BACKEND_URL")),
		ServerID:   strings.TrimSpace(os.Getenv("SERVER_ID")),
		AgentToken: strings.TrimSpace(os.Getenv("AGENT_TOKEN")),
	}

	if cfg.BackendURL == "" {
		return Config{}, errors.New("BACKEND_URL is required")
	}

	if cfg.ServerID == "" {
		return Config{}, errors.New("SERVER_ID is required")
	}

	interval := defaultInterval
	if raw := strings.TrimSpace(os.Getenv("REPORT_INTERVAL")); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid REPORT_INTERVAL: %w", err)
		}
		if seconds <= 0 {
			return Config{}, errors.New("REPORT_INTERVAL must be positive")
		}
		interval = time.Duration(seconds) * time.Second
	}

	if interval < minInterval || interval > maxInterval {
		return Config{}, fmt.Errorf("REPORT_INTERVAL must be between %d and %d seconds", int(minInterval.Seconds()), int(maxInterval.Seconds()))
	}

	cfg.ReportInterval = interval

	return cfg, nil
}
