package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"vpnagent/internal/collector"
	"vpnagent/internal/config"
)

// Sender posts metrics to the backend REST endpoint.
type Sender struct {
	cfg    config.Config
	client *http.Client
}

// New returns a configured sender with sensible HTTP defaults.
func New(cfg config.Config) *Sender {
	return &Sender{
		cfg: cfg,
		client: &http.Client{
			Timeout: 4 * time.Second,
		},
	}
}

// Report transmits the provided metrics in the required JSON format.
func (s *Sender) Report(ctx context.Context, metrics collector.Metrics) error {
	payload := map[string]interface{}{
		"server_id":       s.cfg.ServerID,
		"cpu_idle":        round(metrics.CPUIdle, 1),
		"softirq_percent": round(metrics.SoftIRQPercent, 1),
		"bw_in_mbps":      round(metrics.BWInMbps, 1),
		"bw_out_mbps":     round(metrics.BWOutMbps, 1),
		"timestamp":       metrics.Timestamp,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/metrics/report", s.cfg.BackendURL), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if s.cfg.AgentToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.AgentToken)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send metrics: %w", err)
	}
	defer resp.Body.Close()

	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("backend responded with status %s", resp.Status)
	}

	return nil
}

func round(value float64, precision int) float64 {
	if precision < 0 {
		return value
	}

	pow := math.Pow(10, float64(precision))
	return math.Round(value*pow) / pow
}
