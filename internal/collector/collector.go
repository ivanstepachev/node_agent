package collector

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"vpnagent/internal/netinfo"
)

// Metrics captures a single report payload.
type Metrics struct {
	CPUIdle        float64
	SoftIRQPercent float64
	BWInMbps       float64
	BWOutMbps      float64
	Timestamp      int64
}

// Collector consolidates data from /proc files and interface detection.
type Collector struct {
	detector    netinfo.Detector
	mu          sync.Mutex
	prevCPU     cpuTimes
	prevSoftIRQ uint64
	prevNet     map[string]netCounters
	lastSample  time.Time
}

// New returns a Collector that uses the provided interface detector.
func New(detector netinfo.Detector) *Collector {
	return &Collector{
		detector: detector,
		prevNet:  make(map[string]netCounters),
	}
}

// Collect gathers the latest metrics snapshot.
func (c *Collector) Collect() (Metrics, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	metrics := Metrics{Timestamp: now.Unix()}

	cpuSnap, err := readCPUTimes()
	if err != nil {
		return Metrics{}, fmt.Errorf("read cpu: %w", err)
	}

	ifaces, err := c.detector.ActiveInterfaces()
	if err != nil {
		return Metrics{}, fmt.Errorf("detect interfaces: %w", err)
	}

	netStats, err := readNetDev(ifaces)
	if err != nil {
		return Metrics{}, fmt.Errorf("read netdev: %w", err)
	}

	if c.prevCPU.total > 0 {
		deltaIdle := cpuSnap.idle - c.prevCPU.idle
		deltaTotal := cpuSnap.total - c.prevCPU.total
		if deltaTotal > 0 {
			metrics.CPUIdle = clampPercent(float64(deltaIdle) / float64(deltaTotal) * 100)
		}
	}

	lastSample := c.lastSample
	c.lastSample = now
	elapsedSeconds := 0.0
	if !lastSample.IsZero() {
		elapsedSeconds = now.Sub(lastSample).Seconds()
	}

	softTotal, err := readSoftIRQTotal()
	if err != nil {
		return Metrics{}, fmt.Errorf("read softirq: %w", err)
	}

	if elapsedSeconds > 0 && c.prevSoftIRQ > 0 && softTotal >= c.prevSoftIRQ {
		deltaSoft := softTotal - c.prevSoftIRQ
		metrics.SoftIRQPercent = clampPercent(softIRQRateToPercent(deltaSoft, elapsedSeconds))
	}

	if len(netStats) > 0 && elapsedSeconds > 0 {
		var deltaRx, deltaTx uint64
		for name, curr := range netStats {
			prev := c.prevNet[name]
			if curr.rxBytes >= prev.rxBytes {
				deltaRx += curr.rxBytes - prev.rxBytes
			}
			if curr.txBytes >= prev.txBytes {
				deltaTx += curr.txBytes - prev.txBytes
			}
		}

		metrics.BWInMbps = bytesPerSecondToMbps(deltaRx, elapsedSeconds)
		metrics.BWOutMbps = bytesPerSecondToMbps(deltaTx, elapsedSeconds)
	}

	c.prevCPU = cpuSnap
	c.prevSoftIRQ = softTotal
	c.prevNet = netStats

	return metrics, nil
}

type cpuTimes struct {
	idle  uint64
	total uint64
}

func readCPUTimes() (cpuTimes, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return cpuTimes{}, errors.New("unexpected /proc/stat format")
	}

	line := scanner.Text()
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTimes{}, errors.New("malformed cpu line")
	}

	var total uint64
	values := make([]uint64, len(fields)-1)
	for i, raw := range fields[1:] {
		val, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return cpuTimes{}, err
		}
		values[i] = val
		total += val
	}

	return cpuTimes{
		idle:  values[3], // idle
		total: total,
	}, nil
}

func readSoftIRQTotal() (uint64, error) {
	file, err := os.Open("/proc/softirqs")
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var total uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}

		fields := strings.Fields(parts[1])
		for _, field := range fields {
			val, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return 0, err
			}
			total += val
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return total, nil
}

type netCounters struct {
	rxBytes uint64
	txBytes uint64
}

func readNetDev(interfaces []string) (map[string]netCounters, error) {
	if len(interfaces) == 0 {
		return map[string]netCounters{}, nil
	}

	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Skip headers
	for i := 0; i < 2 && scanner.Scan(); i++ {
	}

	targets := make(map[string]struct{}, len(interfaces))
	for _, name := range interfaces {
		targets[name] = struct{}{}
	}

	stats := make(map[string]netCounters, len(interfaces))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		if len(targets) > 0 {
			if _, ok := targets[iface]; !ok {
				continue
			}
		}

		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}

		rxBytes, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return nil, err
		}

		txBytes, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			return nil, err
		}

		stats[iface] = netCounters{rxBytes: rxBytes, txBytes: txBytes}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return stats, nil
}

func bytesPerSecondToMbps(bytes uint64, seconds float64) float64 {
	if seconds <= 0 {
		return 0
	}
	bits := float64(bytes) * 8
	return bits / 1_000_000 / seconds
}

const softIRQFullScalePerCore = 50_000.0

func softIRQRateToPercent(deltaSoft uint64, seconds float64) float64 {
	if seconds <= 0 {
		return 0
	}
	perSecond := float64(deltaSoft) / seconds

	numCPU := runtime.NumCPU()
	if numCPU < 1 {
		numCPU = 1
	}
	fullScale := float64(numCPU) * softIRQFullScalePerCore
	return perSecond / fullScale * 100
}

func clampPercent(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return v
	}
}
