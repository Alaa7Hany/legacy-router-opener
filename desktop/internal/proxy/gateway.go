package proxy

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	ipconfigGatewayRegex = regexp.MustCompile(`(?i)Default Gateway[^:\r\n]*:\s*(\d{1,3}(?:\.\d{1,3}){3})`)
	macGatewayRegex      = regexp.MustCompile(`(?m)^\s*gateway:\s*(\d{1,3}(?:\.\d{1,3}){3})\s*$`)
)

// DetectDefaultGateway discovers the active LAN default gateway IP without external dependencies,
// falling back to "192.168.1.1" if detection fails or no gateway is configured.
func DetectDefaultGateway() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	switch runtime.GOOS {
	case "windows":
		if out, err := exec.CommandContext(ctx, "route", "print", "0.0.0.0").Output(); err == nil {
			if gw := ParseWindowsRoutePrint(string(out)); gw != "" {
				return gw
			}
		}
		if out, err := exec.CommandContext(ctx, "ipconfig").Output(); err == nil {
			if gw := ParseWindowsIPConfig(string(out)); gw != "" {
				return gw
			}
		}
	case "linux":
		if data, err := os.ReadFile("/proc/net/route"); err == nil {
			if gw := ParseProcNetRoute(string(data)); gw != "" {
				return gw
			}
		}
		if out, err := exec.CommandContext(ctx, "ip", "route", "show", "default").Output(); err == nil {
			if gw := ParseLinuxIPRoute(string(out)); gw != "" {
				return gw
			}
		}
	case "darwin":
		if out, err := exec.CommandContext(ctx, "route", "-n", "get", "default").Output(); err == nil {
			if gw := ParseMacRouteGet(string(out)); gw != "" {
				return gw
			}
		}
	}

	return "192.168.1.1"
}

// ParseWindowsRoutePrint extracts the default IPv4 gateway with the lowest metric from `route print 0.0.0.0`.
func ParseWindowsRoutePrint(output string) string {
	bestGW := ""
	bestMetric := int(^uint(0) >> 1)

	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if fields[0] != "0.0.0.0" || fields[1] != "0.0.0.0" {
			continue
		}
		gw := net.ParseIP(fields[2]).To4()
		if gw == nil || gw.IsUnspecified() {
			continue
		}
		metric, err := strconv.Atoi(fields[4])
		if err != nil {
			metric = bestMetric - 1
		}
		if bestGW == "" || metric < bestMetric {
			bestGW = gw.String()
			bestMetric = metric
		}
	}
	return bestGW
}

// ParseWindowsIPConfig extracts the first valid IPv4 default gateway from `ipconfig` output.
func ParseWindowsIPConfig(output string) string {
	for _, match := range ipconfigGatewayRegex.FindAllStringSubmatch(output, -1) {
		if len(match) < 2 {
			continue
		}
		if ip := net.ParseIP(match[1]).To4(); ip != nil && !ip.IsUnspecified() {
			return ip.String()
		}
	}
	return ""
}

// ParseProcNetRoute extracts the lowest-metric default IPv4 gateway from Linux `/proc/net/route`.
func ParseProcNetRoute(content string) string {
	bestGW := ""
	bestMetric := int(^uint(0) >> 1)

	lines := strings.Split(content, "\n")
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		// Destination and Mask are both 00000000 for the default route.
		if fields[1] != "00000000" || fields[7] != "00000000" {
			continue
		}
		// /proc/net/route prints the 32-bit address in host byte order (%08X).
		val, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil || val == 0 {
			continue
		}
		ip := make(net.IP, 4)
		binary.NativeEndian.PutUint32(ip, uint32(val))
		metric, err := strconv.Atoi(fields[6])
		if err != nil {
			metric = bestMetric - 1
		}
		if bestGW == "" || metric < bestMetric {
			bestGW = ip.String()
			bestMetric = metric
		}
	}
	return bestGW
}

// ParseLinuxIPRoute extracts the default gateway from `ip route show default`.
func ParseLinuxIPRoute(output string) string {
	fields := strings.Fields(output)
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == "via" {
			if ip := net.ParseIP(fields[i+1]).To4(); ip != nil && !ip.IsUnspecified() {
				return ip.String()
			}
		}
	}
	return ""
}

// ParseMacRouteGet extracts the default gateway from `route -n get default` on macOS.
func ParseMacRouteGet(output string) string {
	match := macGatewayRegex.FindStringSubmatch(output)
	if len(match) < 2 {
		return ""
	}
	if ip := net.ParseIP(match[1]).To4(); ip != nil && !ip.IsUnspecified() {
		return ip.String()
	}
	return ""
}
