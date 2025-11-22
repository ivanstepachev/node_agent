package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vpnagent/internal/collector"
	"vpnagent/internal/config"
	"vpnagent/internal/logger"
	"vpnagent/internal/netinfo"
	"vpnagent/internal/sender"
	"vpnagent/internal/xraystats"
)

func main() {
	if len(os.Args) > 1 {
		if err := runCLI(os.Args[1:]); err != nil {
			logger.Fatalf("cli: %v", err)
		}
		return
	}

	runAgent()
}

func runAgent() {
	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	detector := netinfo.NewDetector()
	metricsCollector := collector.New(detector)
	metricsSender := sender.New(cfg)

	logger.Infof("agent started interval=%s backend=%s", cfg.ReportInterval, cfg.BackendURL)

	ticker := time.NewTicker(cfg.ReportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Infof("shutdown signal received, exiting")
			return
		case <-ticker.C:
			metrics, err := metricsCollector.Collect()
			if err != nil {
				logger.Warnf("collect metrics: %v", err)
				continue
			}

			reportCtx, cancel := context.WithTimeout(ctx, cfg.ReportInterval)
			if err := metricsSender.Report(reportCtx, metrics); err != nil {
				logger.Warnf("send metrics: %v", err)
			} else {
				logger.Infof("metrics sent: cpu_idle=%.1f softirq=%.1f bw_in=%.1f bw_out=%.1f", metrics.CPUIdle, metrics.SoftIRQPercent, metrics.BWInMbps, metrics.BWOutMbps)
			}
			cancel()
		}
	}
}

func runCLI(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "online":
		client, err := buildXrayClient()
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), client.SampleInterval()*2+3*time.Second)
		defer cancel()

		count, err := client.CountActiveUsers(ctx)
		if err != nil {
			return fmt.Errorf("fetch active users: %w", err)
		}

		fmt.Printf("Active users: %d\n", count)
		return nil
	case "users":
		client, err := buildXrayClient()
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), client.SampleInterval()*2+5*time.Second)
		defer cancel()

		snapshot, activity, err := client.MeasureUsers(ctx)
		if err != nil {
			return fmt.Errorf("fetch user stats: %w", err)
		}

		printUserSummary(snapshot, activity, client.ThresholdBytes())
		return nil
	case "help", "--help", "-h":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  node-agent            # run metrics agent (default)")
	fmt.Println("  node-agent online     # show number of active Xray users via XRAY_API_ADDR")
	fmt.Println("  node-agent users      # list all clientEmail entries with traffic & active flag")
	fmt.Println("  node-agent help       # show this message")
}

func buildXrayClient() (*xraystats.Client, error) {
	addr := strings.TrimSpace(os.Getenv("XRAY_API_ADDR"))

	var opts []xraystats.Option
	if raw := strings.TrimSpace(os.Getenv("XRAY_ACTIVE_THRESHOLD")); raw != "" {
		val, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse XRAY_ACTIVE_THRESHOLD: %w", err)
		}
		opts = append(opts, xraystats.WithThresholdBytes(val))
	}

	return xraystats.NewClient(addr, opts...), nil
}

func printUserSummary(snapshot map[string]xraystats.UserTraffic, activity map[string]xraystats.UserActivity, threshold uint64) {
	emails := make([]string, 0, len(snapshot))
	for email := range snapshot {
		emails = append(emails, email)
	}
	sort.Strings(emails)

	activeCount := 0
	for _, email := range emails {
		if activity[email].Active(threshold) {
			activeCount++
		}
	}

	fmt.Printf("Total configs: %d\n", len(emails))
	fmt.Printf("Active now:   %d (threshold %s / interval %.0fs)\n\n",
		activeCount, formatBytes(int64(threshold)), xraystats.DefaultSampleInterval.Seconds())

	if len(emails) == 0 {
		fmt.Println("No clientEmail entries reported by Xray.")
		return
	}

	fmt.Printf("%-32s %-12s %-12s %-8s\n", "Email", "Down", "Up", "Status")
	for _, email := range emails {
		traffic := snapshot[email]
		state := activityStatus(activity[email], threshold)
		fmt.Printf("%-32s %-12s %-12s %-8s\n",
			email,
			formatBytes(traffic.Downlink),
			formatBytes(traffic.Uplink),
			state)
	}
}

func activityStatus(a xraystats.UserActivity, threshold uint64) string {
	switch {
	case a.Active(threshold):
		return "active"
	case a.Total() > 0:
		return "burst"
	default:
		return "idle"
	}
}

func formatBytes(v int64) string {
	if v < 0 {
		v = 0
	}
	const step = 1024.0
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	val := float64(v)
	idx := 0
	for idx < len(units)-1 && val >= step {
		val /= step
		idx++
	}
	if idx == 0 {
		return fmt.Sprintf("%d %s", int64(val), units[idx])
	}
	return fmt.Sprintf("%.1f %s", val, units[idx])
}
