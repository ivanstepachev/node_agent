package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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
		addr := strings.TrimSpace(os.Getenv("XRAY_API_ADDR"))
		client := xraystats.NewClient(addr)

		ctx, cancel := context.WithTimeout(context.Background(), xraystats.DefaultSampleInterval*2+3*time.Second)
		defer cancel()

		count, err := client.CountActiveUsers(ctx)
		if err != nil {
			return fmt.Errorf("fetch active users: %w", err)
		}

		fmt.Printf("Active users: %d\n", count)
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
	fmt.Println("  node-agent help       # show this message")
}
