package proxy

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Endpoint describes a resolved upstream router or legacy hardware target.
type Endpoint struct {
	Host                 string
	Port                 int
	UseTLS               bool
	ExplicitPortOrScheme bool
}

// Scheme returns "https" or "http" for this endpoint.
func (e Endpoint) Scheme() string {
	if e.UseTLS {
		return "https"
	}
	return "http"
}

// Authority returns host or host:port, omitting default ports (80 for HTTP, 443 for HTTPS).
func (e Endpoint) Authority() string {
	isDefaultPort := (e.UseTLS && e.Port == 443) || (!e.UseTLS && e.Port == 80)
	if isDefaultPort {
		return e.Host
	}
	return fmt.Sprintf("%s:%d", e.Host, e.Port)
}

// DialAddress returns a host:port string suitable for net.Dialer.
func (e Endpoint) DialAddress() string {
	bareHost := strings.TrimSuffix(strings.TrimPrefix(e.Host, "["), "]")
	return net.JoinHostPort(bareHost, strconv.Itoa(e.Port))
}

// BareHost returns the hostname or IP without IPv6 brackets.
func (e Endpoint) BareHost() string {
	return strings.TrimSuffix(strings.TrimPrefix(e.Host, "["), "]")
}

// URL returns the base URL string (e.g. "https://192.168.1.1" or "http://192.168.1.1:8080").
func (e Endpoint) URL() string {
	return fmt.Sprintf("%s://%s", e.Scheme(), e.Authority())
}

// ParseEndpoint parses user-supplied router input (IP, host:port, or full URL) into an Endpoint.
// When neither a port nor a scheme is provided, it defaults to HTTPS on port 443 (with runtime HTTP :80 fallback).
func ParseEndpoint(raw string) Endpoint {
	trimmed := strings.TrimSpace(raw)
	lower := strings.ToLower(trimmed)

	hasHTTP := strings.HasPrefix(lower, "http://")
	hasHTTPS := strings.HasPrefix(lower, "https://")

	cleaned := trimmed
	if hasHTTP {
		cleaned = cleaned[len("http://"):]
	} else if hasHTTPS {
		cleaned = cleaned[len("https://"):]
	}

	if slashIdx := strings.IndexByte(cleaned, '/'); slashIdx >= 0 {
		cleaned = cleaned[:slashIdx]
	}
	cleaned = strings.TrimSpace(cleaned)

	host := cleaned
	var parsedPort int
	hasPort := false

	if strings.HasPrefix(cleaned, "[") {
		if closeIdx := strings.IndexByte(cleaned, ']'); closeIdx > 0 {
			host = cleaned[:closeIdx+1]
			after := cleaned[closeIdx+1:]
			if strings.HasPrefix(after, ":") {
				if p, ok := parseValidPort(after[1:]); ok {
					parsedPort = p
					hasPort = true
				}
			}
		}
	} else if colonCount := strings.Count(cleaned, ":"); colonCount == 1 {
		colonIdx := strings.LastIndexByte(cleaned, ':')
		if p, ok := parseValidPort(cleaned[colonIdx+1:]); ok {
			host = strings.TrimSpace(cleaned[:colonIdx])
			parsedPort = p
			hasPort = true
		}
	} else if colonCount > 1 {
		if ip := net.ParseIP(cleaned); ip != nil {
			host = "[" + cleaned + "]"
		}
	}

	if host == "" {
		host = "192.168.1.1"
	}

	explicitPortOrScheme := hasHTTP || hasHTTPS || hasPort
	port := parsedPort
	if !hasPort {
		if hasHTTP {
			port = 80
		} else {
			port = 443
		}
	}

	var useTLS bool
	switch {
	case hasHTTP:
		useTLS = false
	case hasHTTPS:
		useTLS = true
	case port == 80:
		useTLS = false
	default:
		useTLS = true
	}

	return Endpoint{
		Host:                 host,
		Port:                 port,
		UseTLS:               useTLS,
		ExplicitPortOrScheme: explicitPortOrScheme,
	}
}

func parseValidPort(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	p, err := strconv.Atoi(s)
	if err != nil || p < 1 || p > 65535 {
		return 0, false
	}
	return p, true
}
