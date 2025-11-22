package netdetect

import (
	"errors"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNoInterfaces = errors.New("no suitable WAN interfaces detected")

	excludedPrefixes = []string{
		"lo", "docker", "br-", "veth", "virbr", "tun", "tap",
		"wg", "zt", "ham",
	}
	cacheTTL = 30 * time.Second

	privateCIDRs = []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10",
		"fd00::/8",
		"fc00::/7",
	}
	privateBlocks []*net.IPNet
)

func init() {
	for _, cidr := range privateCIDRs {
		_, block, err := net.ParseCIDR(cidr)
		if err == nil {
			privateBlocks = append(privateBlocks, block)
		}
	}
}

// Detector discovers WAN interfaces and caches the result for a short period.
type Detector struct {
	mu        sync.Mutex
	cached    []string
	expiresAt time.Time
}

// NewDetector returns a configured detector.
func NewDetector() *Detector {
	return &Detector{}
}

// Detect returns the list of WAN interface names, refreshing the cache if needed.
func (d *Detector) Detect() ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	if len(d.cached) > 0 && now.Before(d.expiresAt) {
		return cloneStrings(d.cached), nil
	}

	ifaces, err := findInterfaces()
	if err != nil {
		return nil, err
	}
	d.cached = ifaces
	d.expiresAt = now.Add(cacheTTL)
	return cloneStrings(d.cached), nil
}

func findInterfaces() ([]string, error) {
	var candidates []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		name := iface.Name

		if shouldExclude(name) {
			continue
		}
		if iface.MTU < 1000 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}

		if !hasGlobalIP(addrs) {
			continue
		}

		candidates = append(candidates, name)
	}

	if len(candidates) == 0 {
		return nil, ErrNoInterfaces
	}

	sort.Strings(candidates)
	return candidates, nil
}

func shouldExclude(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range excludedPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func hasGlobalIP(addrs []net.Addr) bool {
	for _, addr := range addrs {
		ip := extractIP(addr)
		if ip == nil {
			continue
		}
		if isGlobal(ip) {
			return true
		}
	}
	return false
}

func extractIP(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

func isGlobal(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip = ip.To16()
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if isPrivate(ip) {
		return false
	}
	return ip.IsGlobalUnicast()
}

func isPrivate(ip net.IP) bool {
	for _, block := range privateBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

func cloneStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}
