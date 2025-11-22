package collector

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/node_agent/internal/netdetect"
)

var (
	procStatPath    = "/proc/stat"
	procSoftIRQPath = "/proc/softirqs"
	procNetDevPath  = "/proc/net/dev"
)

// Metrics captures the computed values that will be reported to the backend.
type Metrics struct {
	CPUIdlePercent   float64
	SoftIRQPercent   float64
	BandwidthInMbps  float64
	BandwidthOutMbps float64
	Timestamp        time.Time
}

type cpuTimes struct {
	idle  uint64
	total uint64
}

type netCounters struct {
	rxBytes uint64
	txBytes uint64
}

// Collector keeps stateful counters required to compute deltas.
type Collector struct {
	mu        sync.Mutex
	detector  *netdetect.Detector
	lastCPU   cpuTimes
	lastIRQ   uint64
	lastNet   map[string]netCounters
	lastStamp time.Time
	ready     bool
}

// New returns a Collector bound to the provided interface detector.
func New(detector *netdetect.Detector) *Collector {
	return &Collector{
		detector: detector,
		lastNet:  make(map[string]netCounters),
	}
}

// Collect reads the latest kernel counters and returns aggregated metrics.
// The ready flag indicates when deltas are available (after the first cycle).
func (c *Collector) Collect() (*Metrics, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.detector == nil {
		return nil, false, errors.New("interface detector is nil")
	}

	currCPU, err := readCPUTimes()
	if err != nil {
		return nil, false, fmt.Errorf("read cpu times: %w", err)
	}

	currIRQ, err := readSoftIRQTotal()
	if err != nil {
		return nil, false, fmt.Errorf("read softirq: %w", err)
	}

	ifaces, err := c.detector.Detect()
	var ifaceErr error
	if err != nil {
		if errors.Is(err, netdetect.ErrNoInterfaces) {
			ifaceErr = err
		} else {
			return nil, false, fmt.Errorf("detect interfaces: %w", err)
		}
	}

	netMap := make(map[string]netCounters)
	if len(ifaces) > 0 {
		netMap, err = readNetCounters(ifaces)
		if err != nil {
			return nil, false, fmt.Errorf("read net counters: %w", err)
		}
	}

	now := time.Now()
	if !c.ready {
		c.lastCPU = currCPU
		c.lastIRQ = currIRQ
		c.lastNet = netMap
		c.lastStamp = now
		c.ready = true
		return nil, false, nil
	}

	elapsed := now.Sub(c.lastStamp).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	cpuIdle := calculateCPUIdle(c.lastCPU, currCPU)
	softIRQ := calculateSoftIRQPercent(c.lastCPU, currCPU, c.lastIRQ, currIRQ)
	bwIn, bwOut := calculateBandwidth(c.lastNet, netMap, elapsed)

	metrics := &Metrics{
		CPUIdlePercent:   cpuIdle,
		SoftIRQPercent:   softIRQ,
		BandwidthInMbps:  bwIn,
		BandwidthOutMbps: bwOut,
		Timestamp:        now,
	}

	// Update state for next iteration.
	c.lastCPU = currCPU
	c.lastIRQ = currIRQ
	c.lastNet = netMap
	c.lastStamp = now

	// Propagate interface detection error so the caller can log it.
	if ifaceErr != nil {
		return metrics, true, ifaceErr
	}

	return metrics, true, nil
}

func calculateCPUIdle(prev, curr cpuTimes) float64 {
	totalDelta := delta(prev.total, curr.total)
	if totalDelta == 0 {
		return 0
	}
	idleDelta := delta(prev.idle, curr.idle)
	return (float64(idleDelta) / float64(totalDelta)) * 100
}

func calculateSoftIRQPercent(prevCPU, currCPU cpuTimes, prevIRQ, currIRQ uint64) float64 {
	totalDelta := delta(prevCPU.total, currCPU.total)
	if totalDelta == 0 {
		return 0
	}
	softDelta := delta(prevIRQ, currIRQ)
	return (float64(softDelta) / float64(totalDelta)) * 100
}

func calculateBandwidth(prev, curr map[string]netCounters, elapsed float64) (float64, float64) {
	if elapsed <= 0 {
		return 0, 0
	}
	var deltaRx, deltaTx uint64
	for name, counters := range curr {
		if last, ok := prev[name]; ok {
			deltaRx += delta(last.rxBytes, counters.rxBytes)
			deltaTx += delta(last.txBytes, counters.txBytes)
		}
	}

	rxMbps := (float64(deltaRx) * 8) / 1_000_000 / elapsed
	txMbps := (float64(deltaTx) * 8) / 1_000_000 / elapsed
	return rxMbps, txMbps
}

func delta(prev, curr uint64) uint64 {
	if curr >= prev {
		return curr - prev
	}
	// Counter rollover; reset delta to current to avoid negative values.
	return curr
}

func readCPUTimes() (cpuTimes, error) {
	file, err := os.Open(procStatPath)
	if err != nil {
		return cpuTimes{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return cpuTimes{}, errors.New("empty /proc/stat")
	}

	line := scanner.Text()
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTimes{}, errors.New("unexpected /proc/stat format")
	}

	var total uint64
	var idle uint64
	for idx, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuTimes{}, err
		}
		total += value
		if idx == 3 {
			// idle column
			idle = value
		}
		if idx == 4 {
			// iowait column counts as idle time as well
			idle += value
		}
	}

	return cpuTimes{idle: idle, total: total}, nil
}

func readSoftIRQTotal() (uint64, error) {
	file, err := os.Open(procSoftIRQPath)
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
		values := strings.Fields(parts[1])
		for _, v := range values {
			num, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return 0, err
			}
			total += num
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return total, nil
}

func readNetCounters(ifaces []string) (map[string]netCounters, error) {
	file, err := os.Open(procNetDevPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	ifaceSet := make(map[string]struct{}, len(ifaces))
	for _, iface := range ifaces {
		ifaceSet[iface] = struct{}{}
	}

	result := make(map[string]netCounters, len(ifaces))
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		// Skip header lines
		if lineNum <= 2 {
			continue
		}
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if _, ok := ifaceSet[name]; !ok {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			return nil, fmt.Errorf("unexpected /proc/net/dev format for %s", name)
		}
		rx, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return nil, err
		}
		tx, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			return nil, err
		}
		result[name] = netCounters{rxBytes: rx, txBytes: tx}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
