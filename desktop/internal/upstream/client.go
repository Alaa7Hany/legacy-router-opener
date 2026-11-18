package upstream

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrSelfTargetBlocked is returned when a target resolves to a loopback or unspecified address on one of the app's own ports.
var ErrSelfTargetBlocked = errors.New("cannot target the bridge's own local ports")

const (
	defaultConnectTimeout = 10 * time.Second
	defaultIOTimeout      = 15 * time.Second
	defaultMaxRetries     = 2
	retryDelay            = 200 * time.Millisecond
)

// Client is a lenient legacy TLS & HTTP/1.0+1.1 upstream client designed for embedded hardware.
type Client struct {
	limiter *slotLimiter
	onLog   func(string)

	mu                   sync.RWMutex
	httpFallbackHosts    map[string]bool
	legacyTLSHosts       map[string]bool
	blockedLoopbackPorts map[int]bool
	lastTLS              TLSInfo
}

// NewClient constructs a Client with the given max concurrent socket limit and optional logger.
func NewClient(maxSockets int, onLog func(string)) *Client {
	return &Client{
		limiter:              newSlotLimiter(maxSockets),
		onLog:                onLog,
		httpFallbackHosts:    make(map[string]bool),
		legacyTLSHosts:       make(map[string]bool),
		blockedLoopbackPorts: make(map[int]bool),
		lastTLS: TLSInfo{
			Transport: "Idle",
			Protocol:  "—",
		},
	}
}

// SetBlockedLoopbackPorts configures ports (such as the proxy and control ports) that may not be targeted via loopback.
func (c *Client) SetBlockedLoopbackPorts(ports ...int) {
	c.mu.Lock()
	c.blockedLoopbackPorts = make(map[int]bool, len(ports))
	for _, p := range ports {
		if p > 0 {
			c.blockedLoopbackPorts[p] = true
		}
	}
	c.mu.Unlock()
}

func (c *Client) isBlockedPort(port int) bool {
	if port <= 0 {
		return false
	}
	c.mu.RLock()
	blocked := c.blockedLoopbackPorts[port]
	c.mu.RUnlock()
	return blocked
}

// CheckTargetAllowed rejects targets that resolve to loopback/unspecified addresses on any blocked local port.
func (c *Client) CheckTargetAllowed(ctx context.Context, endpoint Endpoint) error {
	if !c.isBlockedPort(endpoint.Port) {
		return nil
	}
	bareHost := endpoint.BareHost()
	if IsLoopbackOrUnspecifiedHost(bareHost) {
		return ErrSelfTargetBlocked
	}
	if net.ParseIP(bareHost) == nil {
		lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if ips, err := net.DefaultResolver.LookupIPAddr(lookupCtx, bareHost); err == nil {
			for _, ipAddr := range ips {
				if ipAddr.IP.IsLoopback() || ipAddr.IP.IsUnspecified() {
					return ErrSelfTargetBlocked
				}
			}
		}
	}
	return nil
}

// SetMaxSockets updates the concurrency cap (e.g. 2 for iLO/APC, 4 for routers).
func (c *Client) SetMaxSockets(n int) {
	c.limiter.SetLimit(n)
}

// MaxSockets returns the active concurrency cap.
func (c *Client) MaxSockets() int {
	return c.limiter.Limit()
}

// LastTLSInfo returns the most recent TLS or HTTP transport telemetry snapshot.
func (c *Client) LastTLSInfo() TLSInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastTLS
}

// ResetState clears remembered per-host fallback entries and resets telemetry.
func (c *Client) ResetState() {
	c.mu.Lock()
	c.httpFallbackHosts = make(map[string]bool)
	c.legacyTLSHosts = make(map[string]bool)
	c.lastTLS = TLSInfo{Transport: "Idle", Protocol: "—"}
	c.mu.Unlock()
}

func (c *Client) log(msg string) {
	if c.onLog != nil {
		c.onLog(msg)
	}
}

func (c *Client) setHTTPFallback(key string, enabled bool) {
	c.mu.Lock()
	if enabled {
		c.httpFallbackHosts[key] = true
	} else {
		delete(c.httpFallbackHosts, key)
	}
	c.mu.Unlock()
}

func (c *Client) hasHTTPFallback(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.httpFallbackHosts[key]
}

func (c *Client) setLegacyTLS(key string, enabled bool) {
	c.mu.Lock()
	if enabled {
		c.legacyTLSHosts[key] = true
	} else {
		delete(c.legacyTLSHosts, key)
	}
	c.mu.Unlock()
}

func (c *Client) prefersLegacyTLS(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.legacyTLSHosts[key]
}

func (c *Client) recordTelemetry(info TLSInfo) {
	c.mu.Lock()
	c.lastTLS = info
	c.mu.Unlock()
}

// SendRequest forwards a request to targetEndpoint using the specified preset and merged cookie header.
func (c *Client) SendRequest(ctx context.Context, targetEndpoint string, preset HardwarePreset, req UpstreamRequest, cookieHeader string) (UpstreamResponse, Endpoint, error) {
	endpoint := ParseEndpoint(targetEndpoint)
	hostKey := endpoint.BareHost()
	portKey := fmt.Sprintf("%s:%d", hostKey, endpoint.Port)

	if !endpoint.ExplicitPortOrScheme && c.hasHTTPFallback(hostKey) {
		endpoint = Endpoint{
			Host:                 endpoint.Host,
			Port:                 80,
			UseTLS:               false,
			ExplicitPortOrScheme: false,
		}
	} else if c.hasHTTPFallback(portKey) && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(targetEndpoint)), "https://") {
		endpoint.UseTLS = false
	}

	if err := c.CheckTargetAllowed(ctx, endpoint); err != nil {
		return UpstreamResponse{}, endpoint, err
	}

	var lastErr error
	for attempt := 0; attempt <= defaultMaxRetries; attempt++ {
		if err := c.limiter.Acquire(ctx); err != nil {
			return UpstreamResponse{}, endpoint, err
		}

		resp, usedEndpoint, err := c.executeWithFallbacks(ctx, targetEndpoint, endpoint, preset, req, cookieHeader)
		c.limiter.Release()

		if err == nil {
			return resp, usedEndpoint, nil
		}

		lastErr = err
		if attempt < defaultMaxRetries && isRetryableError(err) && ctx.Err() == nil {
			c.log(fmt.Sprintf("[RETRY %d/%d] %s %s (%v)", attempt+1, defaultMaxRetries, req.Method, RedactQuery(req.Path), err))
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return UpstreamResponse{}, usedEndpoint, ctx.Err()
			}
			endpoint = usedEndpoint
			continue
		}

		return UpstreamResponse{}, usedEndpoint, err
	}

	if lastErr == nil {
		lastErr = errors.New("upstream request failed")
	}
	return UpstreamResponse{}, endpoint, lastErr
}

func (c *Client) executeWithFallbacks(ctx context.Context, rawTarget string, endpoint Endpoint, preset HardwarePreset, req UpstreamRequest, cookieHeader string) (UpstreamResponse, Endpoint, error) {
	hostKey := endpoint.BareHost()
	portKey := fmt.Sprintf("%s:%d", hostKey, endpoint.Port)
	preferLegacy := preset.PreferLegacyTLS || c.prefersLegacyTLS(portKey)

	resp, err := c.executeSingle(ctx, endpoint, preferLegacy, req, cookieHeader)
	if err == nil {
		return resp, endpoint, nil
	}
	if errors.Is(err, ErrSelfTargetBlocked) {
		return UpstreamResponse{}, endpoint, err
	}

	// If modern TLS 1.3 ClientHello (e.g., ML-KEM / extra extensions) was rejected by old firmware,
	// immediately retry with a stripped TLS 1.0–1.2 legacy ClientHello and remember it per host:port.
	if endpoint.UseTLS && !preferLegacy && isTLSHandshakeAlertOrReset(err) {
		c.log(fmt.Sprintf("[TLS] Modern ClientHello rejected by %s; switching to TLS 1.0-1.2 legacy profile", endpoint.DialAddress()))
		if legacyResp, legacyErr := c.executeSingle(ctx, endpoint, true, req, cookieHeader); legacyErr == nil {
			c.setLegacyTLS(portKey, true)
			return legacyResp, endpoint, nil
		} else {
			err = legacyErr
		}
	}

	// If a custom port without explicit https:// responded with plaintext HTTP to our TLS ClientHello,
	// automatically fall back to plain HTTP on that same port.
	explicitHTTPS := strings.HasPrefix(strings.ToLower(strings.TrimSpace(rawTarget)), "https://")
	var recordErr tls.RecordHeaderError
	if endpoint.UseTLS && !explicitHTTPS && errors.As(err, &recordErr) {
		c.log(fmt.Sprintf("[HTTP] Port %d on %s spoke plain HTTP instead of TLS; switching to HTTP", endpoint.Port, endpoint.Host))
		httpEndpoint := Endpoint{
			Host:                 endpoint.Host,
			Port:                 endpoint.Port,
			UseTLS:               false,
			ExplicitPortOrScheme: endpoint.ExplicitPortOrScheme,
		}
		if httpResp, httpErr := c.executeSingle(ctx, httpEndpoint, false, req, cookieHeader); httpErr == nil {
			c.setHTTPFallback(portKey, true)
			return httpResp, httpEndpoint, nil
		}
	}

	// Default HTTPS (:443) -> HTTP (:80) fallback when no explicit port or scheme was specified.
	if !endpoint.ExplicitPortOrScheme && endpoint.UseTLS && endpoint.Port == 443 && (isConnectionRefusedError(err) || isImmediateResetError(err)) {
		c.log(fmt.Sprintf("[HTTP] HTTPS on %s:443 refused/reset; falling back to plain HTTP on port 80", endpoint.Host))
		httpEndpoint := Endpoint{
			Host:                 endpoint.Host,
			Port:                 80,
			UseTLS:               false,
			ExplicitPortOrScheme: false,
		}
		httpResp, httpErr := c.executeSingle(ctx, httpEndpoint, false, req, cookieHeader)
		if httpErr == nil {
			c.setHTTPFallback(hostKey, true)
			return httpResp, httpEndpoint, nil
		}
		return UpstreamResponse{}, httpEndpoint, httpErr
	}

	// If a previously cached HTTP :80 fallback now refuses connection, clear cache and retry :443.
	if !endpoint.ExplicitPortOrScheme && !endpoint.UseTLS && endpoint.Port == 80 && isConnectionRefusedError(err) {
		c.setHTTPFallback(hostKey, false)
		c.log(fmt.Sprintf("[TLS] Cached HTTP port 80 refused on %s; retrying HTTPS on port 443", endpoint.Host))
		httpsEndpoint := Endpoint{
			Host:                 endpoint.Host,
			Port:                 443,
			UseTLS:               true,
			ExplicitPortOrScheme: false,
		}
		httpsResp, httpsErr := c.executeSingle(ctx, httpsEndpoint, preferLegacy, req, cookieHeader)
		return httpsResp, httpsEndpoint, httpsErr
	}

	return UpstreamResponse{}, endpoint, err
}

func (c *Client) executeSingle(ctx context.Context, endpoint Endpoint, preferLegacyTLS bool, req UpstreamRequest, cookieHeader string) (UpstreamResponse, error) {
	if err := c.CheckTargetAllowed(ctx, endpoint); err != nil {
		return UpstreamResponse{}, err
	}

	dialer := &net.Dialer{
		Timeout: defaultConnectTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, portStr, splitErr := net.SplitHostPort(address)
			if splitErr != nil {
				return nil
			}
			port, convErr := strconv.Atoi(portStr)
			if convErr != nil || !c.isBlockedPort(port) {
				return nil
			}
			if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsUnspecified()) {
				return ErrSelfTargetBlocked
			}
			return nil
		},
	}
	rawConn, err := dialer.DialContext(ctx, "tcp", endpoint.DialAddress())
	if err != nil {
		return UpstreamResponse{}, err
	}
	defer rawConn.Close()

	if tcpConn, ok := rawConn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
	}

	var rw net.Conn = rawConn
	if endpoint.UseTLS {
		tlsCfg := buildLegacyTLSConfig(preferLegacyTLS)
		tlsConn := tls.Client(rawConn, tlsCfg)
		handshakeCtx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
		err = tlsConn.HandshakeContext(handshakeCtx)
		cancel()
		if err != nil {
			_ = tlsConn.Close()
			return UpstreamResponse{}, err
		}
		rw = tlsConn
		c.recordTelemetry(extractTLSInfo(tlsConn.ConnectionState(), endpoint))
	} else {
		c.recordTelemetry(TLSInfo{
			Transport: "Plain HTTP",
			Protocol:  "HTTP/1.x (No TLS)",
			Target:    endpoint.URL(),
			UpdatedAt: time.Now().Format("15:04:05"),
		})
	}

	_ = rw.SetDeadline(time.Now().Add(defaultIOTimeout))

	wireReq := buildRawHTTPRequest(endpoint, req, cookieHeader)
	if _, err := rw.Write(wireReq); err != nil {
		return UpstreamResponse{}, err
	}

	reader := bufio.NewReaderSize(rw, 16*1024)
	resp, err := parseRawHTTPResponse(reader, req.Method)
	if err != nil {
		return UpstreamResponse{}, err
	}
	return resp, nil
}

func isConnectionRefusedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "actively refused") ||
		strings.Contains(msg, "econnrefused")
}

func isImmediateResetError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "forcibly closed") ||
		strings.Contains(msg, "econnreset")
}

func isTLSHandshakeAlertOrReset(err error) bool {
	if err == nil || isConnectionRefusedError(err) || errors.Is(err, ErrSelfTargetBlocked) {
		return false
	}
	var recordErr tls.RecordHeaderError
	if errors.As(err, &recordErr) {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "tls:") ||
		strings.Contains(msg, "remote error:") ||
		strings.Contains(msg, "handshake") ||
		isImmediateResetError(err)
}

func isRetryableError(err error) bool {
	if err == nil || isConnectionRefusedError(err) || errors.Is(err, ErrSelfTargetBlocked) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return isImmediateResetError(err) || strings.Contains(strings.ToLower(err.Error()), "timed out")
}
