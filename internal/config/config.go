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
	defaultReportInterval = 5 * time.Second
	minReportInterval     = 3 * time.Second
	maxReportInterval     = 30 * time.Second
)

// Config contains runtime settings pulled from the environment.
type Config struct {
	BackendURL     string
	ServerID       string
	ReportInterval time.Duration
	AgentToken     string
}

// Load parses the environment variables and returns a validated Config.
func Load() (*Config, error) {
	cfg := &Config{
		BackendURL: strings.TrimSpace(os.Getenv("BACKEND_URL")),
		ServerID:   strings.TrimSpace(os.Getenv("SERVER_ID")),
		AgentToken: strings.TrimSpace(os.Getenv("AGENT_TOKEN")),
	}

	if cfg.BackendURL == "" {
		return nil, errors.New("BACKEND_URL is required")
	}
	if !strings.HasPrefix(cfg.BackendURL, "http://") && !strings.HasPrefix(cfg.BackendURL, "https://") {
		return nil, errors.New("BACKEND_URL must include scheme (http or https)")
	}
	cfg.BackendURL = strings.TrimRight(cfg.BackendURL, "/")

	if cfg.ServerID == "" {
		return nil, errors.New("SERVER_ID is required")
	}

	intervalStr := strings.TrimSpace(os.Getenv("REPORT_INTERVAL"))
	switch {
	case intervalStr == "":
		cfg.ReportInterval = defaultReportInterval
	default:
		seconds, err := strconv.Atoi(intervalStr)
		if err != nil || seconds <= 0 {
			return nil, fmt.Errorf("invalid REPORT_INTERVAL: %w", err)
		}
		cfg.ReportInterval = time.Duration(seconds) * time.Second
	}

	// Keep the interval inside the safe range to avoid DoS on backend.
	if cfg.ReportInterval < minReportInterval {
		cfg.ReportInterval = minReportInterval
	}
	if cfg.ReportInterval > maxReportInterval {
		cfg.ReportInterval = maxReportInterval
	}

	return cfg, nil
}

// MetricsEndpoint returns the fully-qualified metrics endpoint URL.
func (c *Config) MetricsEndpoint() string {
	return c.BackendURL + "/metrics/report"
}
