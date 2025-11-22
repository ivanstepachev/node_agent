package xraystats

import (
	"context"
	"strings"
	"time"

	"github.com/xtls/xray-core/app/stats/command"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// DefaultAddress is the local Xray API endpoint.
	DefaultAddress = "127.0.0.1:10085"
	// DefaultSampleInterval controls how long we wait between snapshots.
	DefaultSampleInterval = 2 * time.Second
	// DefaultThresholdBytes filters out tiny deltas so idle users are not counted as active.
	DefaultThresholdBytes = 1024
)

// Client talks to the Xray stats API to derive online user counts.
type Client struct {
	addr           string
	sampleInterval time.Duration
	thresholdBytes uint64
}

// Option configures a Client.
type Option func(*Client)

// WithSampleInterval overrides the default sampling interval.
func WithSampleInterval(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.sampleInterval = d
		}
	}
}

// WithThresholdBytes overrides the minimum traffic delta to count a user as active.
func WithThresholdBytes(b uint64) Option {
	return func(c *Client) {
		if b > 0 {
			c.thresholdBytes = b
		}
	}
}

// NewClient builds a stats client.
func NewClient(addr string, opts ...Option) *Client {
	c := &Client{
		addr:           addr,
		sampleInterval: DefaultSampleInterval,
		thresholdBytes: DefaultThresholdBytes,
	}
	if c.addr == "" {
		c.addr = DefaultAddress
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// CountActiveUsers samples Xray stats twice and counts clientEmail entries with meaningful traffic.
func (c *Client) CountActiveUsers(ctx context.Context) (int, error) {
	conn, err := grpc.DialContext(ctx, c.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	client := command.NewStatsServiceClient(conn)

	first, err := c.snapshot(ctx, client)
	if err != nil {
		return 0, err
	}

	if err := wait(ctx, c.sampleInterval); err != nil {
		return 0, err
	}

	second, err := c.snapshot(ctx, client)
	if err != nil {
		return 0, err
	}

	return countActive(first, second, c.thresholdBytes), nil
}

func (c *Client) snapshot(ctx context.Context, client command.StatsServiceClient) (map[string]int64, error) {
	resp, err := client.QueryStats(ctx, &command.QueryStatsRequest{
		Pattern: "user>>>*>>>traffic>>>downlink",
		Reset_:  false,
	})
	if err != nil {
		return nil, err
	}

	stats := make(map[string]int64)
	for _, stat := range resp.GetStat() {
		if stat == nil {
			continue
		}

		user := extractUser(stat.GetName())
		if user == "" {
			continue
		}

		stats[user] = stat.GetValue()
	}

	return stats, nil
}

func countActive(prev, curr map[string]int64, threshold uint64) int {
	if threshold == 0 {
		threshold = DefaultThresholdBytes
	}

	var active int
	for user, currVal := range curr {
		prevVal := prev[user]
		if currVal <= prevVal {
			continue
		}
		if uint64(currVal-prevVal) < threshold {
			continue
		}
		active++
	}

	return active
}

func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func extractUser(statName string) string {
	parts := strings.Split(statName, ">>>")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}
