package main

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"vpnagent/internal/collector"
	"vpnagent/internal/config"
	"vpnagent/internal/logger"
	"vpnagent/internal/netinfo"
	"vpnagent/internal/sender"
)

func main() {
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
