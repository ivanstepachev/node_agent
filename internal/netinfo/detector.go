package netinfo

import (
	"net"
	"sort"
	"strings"
)

var excludePrefixes = []string{
	"lo",
	"docker",
	"br-",
	"veth",
	"virbr",
	"tun",
	"tap",
	"wg",
	"zt",
	"ham",
}

// Detector exposes WAN interface discovery.
type Detector interface {
	ActiveInterfaces() ([]string, error)
}

// SystemDetector inspects local network interfaces directly from the OS.
type SystemDetector struct{}

// NewDetector returns a detector suitable for production workloads.
func NewDetector() *SystemDetector {
	return &SystemDetector{}
}

// ActiveInterfaces returns a deterministic list of usable WAN interfaces.
func (d *SystemDetector) ActiveInterfaces() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var selected []string

ifaceLoop:
	for _, iface := range ifaces {
		if shouldSkip(iface.Name) {
			continue
		}

		if iface.MTU < 1000 {
			continue
		}

		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		hasGlobal := false
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if isGlobalInternetIP(ipNet.IP) {
				hasGlobal = true
				break
			}
		}

		if !hasGlobal {
			continue ifaceLoop
		}

		selected = append(selected, iface.Name)
	}

	sort.Strings(selected)
	return selected, nil
}

func shouldSkip(name string) bool {
	for _, prefix := range excludePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func isGlobalInternetIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	if ip.IsLoopback() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() {
		return false
	}

	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}

	return true
}
