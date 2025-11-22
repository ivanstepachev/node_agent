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
	users, activity, err := c.measureUsers(ctx)
	if err != nil {
		return 0, err
	}

	var active int
	for email := range users {
		if activity[email].Active(c.thresholdBytes) {
			active++
		}
	}

	return active, nil
}

// Snapshot collects current user traffic counters.
func (c *Client) Snapshot(ctx context.Context) (map[string]UserTraffic, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return c.snapshot(ctx, command.NewStatsServiceClient(conn))
}

// MeasureUsers returns the latest snapshot plus per-user activity deltas.
func (c *Client) MeasureUsers(ctx context.Context) (map[string]UserTraffic, map[string]UserActivity, error) {
	return c.measureUsers(ctx)
}

func (c *Client) measureUsers(ctx context.Context) (map[string]UserTraffic, map[string]UserActivity, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer conn.Close()

	client := command.NewStatsServiceClient(conn)

	first, err := c.snapshot(ctx, client)
	if err != nil {
		return nil, nil, err
	}

	if err := wait(ctx, c.sampleInterval); err != nil {
		return nil, nil, err
	}

	second, err := c.snapshot(ctx, client)
	if err != nil {
		return nil, nil, err
	}

	return second, diffActivity(first, second), nil
}

type UserTraffic struct {
	Downlink int64
	Uplink   int64
}

type UserActivity struct {
	DownDelta uint64
	UpDelta   uint64
}

func (a UserActivity) Total() uint64 {
	return a.DownDelta + a.UpDelta
}

func (a UserActivity) Active(threshold uint64) bool {
	if threshold == 0 {
		threshold = DefaultThresholdBytes
	}
	return a.Total() >= threshold
}

func (c *Client) snapshot(ctx context.Context, client command.StatsServiceClient) (map[string]UserTraffic, error) {
	resp, err := client.QueryStats(ctx, &command.QueryStatsRequest{
		Pattern: "user>>>",
		Reset_:  false,
	})
	if err != nil {
		return nil, err
	}

	stats := make(map[string]UserTraffic)
	for _, stat := range resp.GetStat() {
		if stat == nil {
			continue
		}

		email, direction := parseUserStat(stat.GetName())
		if email == "" {
			continue
		}

		entry := stats[email]
		switch direction {
		case "downlink":
			entry.Downlink = stat.GetValue()
		case "uplink":
			entry.Uplink = stat.GetValue()
		default:
			continue
		}
		stats[email] = entry
	}

	return stats, nil
}

func diffActivity(prev, curr map[string]UserTraffic) map[string]UserActivity {
	result := make(map[string]UserActivity, len(curr))
	for email, currVals := range curr {
		prevVals := prev[email]

		var delta UserActivity
		if currVals.Downlink > prevVals.Downlink {
			delta.DownDelta = uint64(currVals.Downlink - prevVals.Downlink)
		}
		if currVals.Uplink > prevVals.Uplink {
			delta.UpDelta = uint64(currVals.Uplink - prevVals.Uplink)
		}

		result[email] = delta
	}
	return result
}

func parseUserStat(name string) (email, direction string) {
	parts := strings.Split(name, ">>>")
	if len(parts) < 4 {
		return "", ""
	}
	if parts[0] != "user" || parts[2] != "traffic" {
		return "", ""
	}
	return parts[1], parts[3]
}

func (c *Client) dial(ctx context.Context) (*grpc.ClientConn, error) {
	addr := c.addr
	if addr == "" {
		addr = DefaultAddress
	}
	return grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// SampleInterval exposes the currently configured sampling delay.
func (c *Client) SampleInterval() time.Duration {
	return c.sampleInterval
}

// ThresholdBytes returns the bytes delta required to treat a user as active.
func (c *Client) ThresholdBytes() uint64 {
	if c.thresholdBytes == 0 {
		return DefaultThresholdBytes
	}
	return c.thresholdBytes
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
