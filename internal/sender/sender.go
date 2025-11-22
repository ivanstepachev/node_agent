package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/example/node_agent/internal/collector"
)

// Sender is responsible for encoding and delivering metrics to the backend.
type Sender struct {
	client   *http.Client
	endpoint string
	serverID string
	token    string
}

// Payload describes the JSON body expected by the backend.
type Payload struct {
	ServerID       string  `json:"server_id"`
	CPUIDLE        float64 `json:"cpu_idle"`
	SoftIRQPercent float64 `json:"softirq_percent"`
	BandwidthIn    float64 `json:"bw_in_mbps"`
	BandwidthOut   float64 `json:"bw_out_mbps"`
	Timestamp      int64   `json:"timestamp"`
}

// New returns a configured Sender.
func New(endpoint, serverID, token string, timeout time.Duration) *Sender {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Sender{
		client: &http.Client{
			Timeout: timeout,
		},
		endpoint: endpoint,
		serverID: serverID,
		token:    token,
	}
}

// Send posts the metrics payload to the backend endpoint.
func (s *Sender) Send(ctx context.Context, metrics *collector.Metrics) error {
	if metrics == nil {
		return fmt.Errorf("metrics payload is nil")
	}
	body := Payload{
		ServerID:       s.serverID,
		CPUIDLE:        round(metrics.CPUIdlePercent),
		SoftIRQPercent: round(metrics.SoftIRQPercent),
		BandwidthIn:    round(metrics.BandwidthInMbps),
		BandwidthOut:   round(metrics.BandwidthOutMbps),
		Timestamp:      metrics.Timestamp.Unix(),
	}

	buffer := &bytes.Buffer{}
	if err := json.NewEncoder(buffer).Encode(body); err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, buffer)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("backend returned status %s", resp.Status)
	}
	return nil
}

func round(val float64) float64 {
	return math.Round(val*10) / 10
}
