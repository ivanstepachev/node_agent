package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/node_agent/internal/collector"
	"github.com/example/node_agent/internal/config"
	"github.com/example/node_agent/internal/logging"
	"github.com/example/node_agent/internal/netdetect"
	"github.com/example/node_agent/internal/sender"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		logging.Error("config error: %v", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	detector := netdetect.NewDetector()
	metricsCollector := collector.New(detector)
	metricsSender := sender.New(cfg.MetricsEndpoint(), cfg.ServerID, cfg.AgentToken, 5*time.Second)

	if _, _, err := metricsCollector.Collect(); err != nil {
		logging.Warn("initial metrics sample failed: %v", err)
	}

	interval := cfg.ReportInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logging.Info("agent started: server=%s interval=%s endpoint=%s", cfg.ServerID, interval, cfg.MetricsEndpoint())

	for {
		select {
		case <-ctx.Done():
			logging.Info("shutdown signal received, exiting")
			return
		case <-ticker.C:
			runCycle(ctx, metricsCollector, metricsSender)
		}
	}
}

func runCycle(ctx context.Context, metricsCollector *collector.Collector, metricsSender *sender.Sender) {
	metrics, ready, err := metricsCollector.Collect()
	if err != nil {
		if errors.Is(err, netdetect.ErrNoInterfaces) {
			logging.Warn("no WAN interfaces detected: %v", err)
		} else {
			logging.Warn("collect error: %v", err)
		}
		if metrics == nil {
			return
		}
	}
	if !ready || metrics == nil {
		// Need at least one delta cycle.
		return
	}

	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := metricsSender.Send(sendCtx, metrics); err != nil {
		logging.Warn("backend unavailable: %v", err)
		return
	}

	logging.Info(
		"metrics sent: cpu_idle=%.1f%% softirq=%.1f%% bw_in=%.1fMbps bw_out=%.1fMbps",
		metrics.CPUIdlePercent,
		metrics.SoftIRQPercent,
		metrics.BandwidthInMbps,
		metrics.BandwidthOutMbps,
	)
}
