package upstream

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode"
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

// ValidateTarget strictly validates user-supplied target input and returns the parsed Endpoint.
func ValidateTarget(raw string) (Endpoint, error) {
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return Endpoint{}, errors.New("target endpoint cannot contain control characters")
		}
	}

	trimmed := strings.Trim(raw, " ")
	if trimmed == "" {
		return Endpoint{}, errors.New("target endpoint cannot be empty")
	}
	for _, r := range trimmed {
		if unicode.IsSpace(r) {
			return Endpoint{}, errors.New("target endpoint cannot contain whitespace")
		}
	}
	if strings.ContainsAny(trimmed, "@<>\"'\\?#") {
		return Endpoint{}, errors.New("target endpoint contains unsupported characters")
	}

	rest := trimmed
	if idx := strings.Index(trimmed, "://"); idx >= 0 {
		scheme := strings.ToLower(trimmed[:idx])
		if scheme != "http" && scheme != "https" {
			return Endpoint{}, errors.New("target scheme must be http:// or https://")
		}
		rest = trimmed[idx+3:]
	}

	authority, _, _ := strings.Cut(rest, "/")
	if authority == "" {
		return Endpoint{}, errors.New("target host cannot be empty")
	}

	if err := validateAuthority(authority); err != nil {
		return Endpoint{}, err
	}

	return ParseEndpoint(trimmed), nil
}

func validateAuthority(authority string) error {
	if strings.HasPrefix(authority, "[") {
		closeIdx := strings.IndexByte(authority, ']')
		if closeIdx <= 1 {
			return errors.New("invalid IPv6 address in target")
		}
		ipStr := authority[1:closeIdx]
		ip := net.ParseIP(ipStr)
		if ip == nil || ip.To4() != nil {
			return errors.New("invalid IPv6 address in target")
		}
		after := authority[closeIdx+1:]
		if after != "" {
			if !strings.HasPrefix(after, ":") {
				return errors.New("invalid port in target")
			}
			if _, ok := parseValidPort(after[1:]); !ok {
				return errors.New("invalid port in target (must be 1-65535)")
			}
		}
		return nil
	}

	if strings.Count(authority, ":") > 1 {
		ip := net.ParseIP(authority)
		if ip == nil || ip.To4() != nil {
			return errors.New("invalid host or IPv6 address in target")
		}
		return nil
	}

	host := authority
	if colonIdx := strings.IndexByte(authority, ':'); colonIdx >= 0 {
		host = authority[:colonIdx]
		portStr := authority[colonIdx+1:]
		if _, ok := parseValidPort(portStr); !ok {
			return errors.New("invalid port in target (must be 1-65535)")
		}
	}
	if host == "" || strings.ContainsAny(host, "[]") {
		return errors.New("invalid target host")
	}

	return validateHost(host)
}

func validateHost(host string) error {
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		return nil
	}
	if isDigitsAndDotsOnly(host) {
		return errors.New("invalid IPv4 address in target")
	}
	if len(host) > 253 {
		return errors.New("target hostname is too long")
	}
	for label := range strings.SplitSeq(host, ".") {
		if len(label) == 0 || len(label) > 63 {
			return errors.New("invalid target hostname")
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("invalid target hostname")
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return errors.New("invalid character in target hostname")
			}
		}
	}
	return nil
}

func isDigitsAndDotsOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if (s[i] < '0' || s[i] > '9') && s[i] != '.' {
			return false
		}
	}
	return len(s) > 0
}

// IsLoopbackOrUnspecifiedHost reports whether host is a literal loopback or unspecified address.
func IsLoopbackOrUnspecifiedHost(host string) bool {
	bare := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(host), "["), "]")
	bare = strings.TrimSuffix(strings.ToLower(bare), ".")
	if bare == "localhost" || strings.HasSuffix(bare, ".localhost") {
		return true
	}
	if ip := net.ParseIP(bare); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	return false
}

// RedactQuery strips query parameters from URLs or paths for safe diagnostic logging.
func RedactQuery(raw string) string {
	idx := strings.IndexByte(raw, '?')
	if idx < 0 {
		return raw
	}
	return raw[:idx] + "?<redacted>"
}

func parseValidPort(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	p, err := strconv.Atoi(s)
	if err != nil || p < 1 || p > 65535 {
		return 0, false
	}
	return p, true
}
