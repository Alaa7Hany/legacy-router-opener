package proxy

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultConnectTimeout = 10 * time.Second
	defaultIOTimeout      = 15 * time.Second
	defaultMaxRetries     = 2
	retryDelay            = 200 * time.Millisecond
)

var (
	proxyAuthorityRegex = regexp.MustCompile(`(?i)https?://(127\.0\.0\.1|localhost|\[::1\])(:\d+)?`)

	strippedOutgoingHeaders = map[string]bool{
		"host":                      true,
		"referer":                   true,
		"origin":                    true,
		"accept":                    true,
		"accept-encoding":           true,
		"connection":                true,
		"keep-alive":                true,
		"proxy-connection":          true,
		"cookie":                    true,
		"content-length":            true,
		"content-type":              true,
		"if-modified-since":         true,
		"if-none-match":             true,
		"upgrade-insecure-requests": true,
	}
)

// TLSInfo captures live diagnostic details from the most recent upstream handshake or HTTP connection.
type TLSInfo struct {
	Transport     string `json:"transport"`
	Protocol      string `json:"protocol"`
	CipherSuite   string `json:"cipherSuite"`
	PeerSubject   string `json:"peerSubject"`
	PeerKeyBits   string `json:"peerKeyBits"`
	CertNotBefore string `json:"certNotBefore"`
	CertNotAfter  string `json:"certNotAfter"`
	CertExpired   bool   `json:"certExpired"`
	Target        string `json:"target"`
	UpdatedAt     string `json:"updatedAt"`
}

// UpstreamRequest represents a normalized browser request to forward upstream.
type UpstreamRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

// UpstreamResponse represents a raw response parsed leniently from the legacy device.
type UpstreamResponse struct {
	StatusCode int
	StatusText string
	Headers    http.Header
	Body       []byte
}

// SlotLimiter bounds concurrent upstream sockets cleanly and supports live preset limit updates.
type SlotLimiter struct {
	mu     sync.Mutex
	cond   *sync.Cond
	limit  int
	active int
}

// NewSlotLimiter creates a concurrency limiter with the specified capacity (>= 1).
func NewSlotLimiter(limit int) *SlotLimiter {
	if limit < 1 {
		limit = 1
	}
	s := &SlotLimiter{limit: limit}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// SetLimit adjusts the maximum number of concurrent slots and wakes waiting callers.
func (s *SlotLimiter) SetLimit(limit int) {
	if limit < 1 {
		limit = 1
	}
	s.mu.Lock()
	s.limit = limit
	s.cond.Broadcast()
	s.mu.Unlock()
}

// Limit returns the current concurrency cap.
func (s *SlotLimiter) Limit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limit
}

// Acquire blocks until a slot is available or ctx is cancelled.
func (s *SlotLimiter) Acquire(ctx context.Context) error {
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.cond.Broadcast()
			s.mu.Unlock()
		case <-done:
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()
	for s.active >= s.limit {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.cond.Wait()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.active++
	return nil
}

// Release frees one slot and wakes a waiting caller.
func (s *SlotLimiter) Release() {
	s.mu.Lock()
	if s.active > 0 {
		s.active--
	}
	s.cond.Signal()
	s.mu.Unlock()
}

// Client is a lenient legacy TLS & HTTP/1.0+1.1 upstream client designed for embedded hardware.
type Client struct {
	limiter *SlotLimiter
	onLog   func(string)

	mu                sync.RWMutex
	httpFallbackHosts map[string]bool
	legacyTLSHosts    map[string]bool
	lastTLS           TLSInfo
}

// NewClient constructs a Client with the given max concurrent socket limit and optional logger.
func NewClient(maxSockets int, onLog func(string)) *Client {
	return &Client{
		limiter:           NewSlotLimiter(maxSockets),
		onLog:             onLog,
		httpFallbackHosts: make(map[string]bool),
		legacyTLSHosts:    make(map[string]bool),
		lastTLS: TLSInfo{
			Transport: "Idle",
			Protocol:  "—",
		},
	}
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
			c.log(fmt.Sprintf("[RETRY %d/%d] %s %s (%v)", attempt+1, defaultMaxRetries, req.Method, req.Path, err))
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
	dialer := &net.Dialer{Timeout: defaultConnectTimeout}
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

func buildLegacyTLSConfig(preferLegacy bool) *tls.Config {
	stdSuites := tls.CipherSuites()
	insecureSuites := tls.InsecureCipherSuites()

	allSuites := make([]uint16, 0, len(stdSuites)+len(insecureSuites))
	if preferLegacy {
		for _, s := range insecureSuites {
			allSuites = append(allSuites, s.ID)
		}
		for _, s := range stdSuites {
			allSuites = append(allSuites, s.ID)
		}
	} else {
		for _, s := range stdSuites {
			allSuites = append(allSuites, s.ID)
		}
		for _, s := range insecureSuites {
			allSuites = append(allSuites, s.ID)
		}
	}

	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS10,
		MaxVersion:         tls.VersionTLS13,
		CipherSuites:       allSuites,
		InsecureSkipVerify: true,
		Renegotiation:      tls.RenegotiateFreelyAsClient,
	}

	if preferLegacy {
		// Strip TLS 1.3 and post-quantum curves so fragile embedded servers receive a compact ClientHello.
		cfg.MaxVersion = tls.VersionTLS12
		cfg.SessionTicketsDisabled = true
		cfg.CurvePreferences = []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
			tls.CurveP384,
		}
	}

	return cfg
}

func extractTLSInfo(cs tls.ConnectionState, endpoint Endpoint) TLSInfo {
	info := TLSInfo{
		Transport:   "HTTPS",
		Protocol:    tlsVersionName(cs.Version),
		CipherSuite: tls.CipherSuiteName(cs.CipherSuite),
		Target:      endpoint.URL(),
		UpdatedAt:   time.Now().Format("15:04:05"),
	}

	if len(cs.PeerCertificates) > 0 {
		cert := cs.PeerCertificates[0]
		subject := cert.Subject.CommonName
		if subject == "" {
			subject = cert.Subject.String()
		}
		if subject == "" {
			subject = "(self-signed / blank subject)"
		}
		info.PeerSubject = subject
		info.CertNotBefore = cert.NotBefore.UTC().Format("2006-01-02")
		info.CertNotAfter = cert.NotAfter.UTC().Format("2006-01-02")
		now := time.Now()
		info.CertExpired = now.After(cert.NotAfter) || now.Before(cert.NotBefore)

		switch pub := cert.PublicKey.(type) {
		case *rsa.PublicKey:
			info.PeerKeyBits = fmt.Sprintf("%d-bit RSA", pub.N.BitLen())
		case *ecdsa.PublicKey:
			info.PeerKeyBits = fmt.Sprintf("%d-bit ECDSA", pub.Curve.Params().BitSize)
		default:
			info.PeerKeyBits = string(cert.PublicKeyAlgorithm.String())
		}
	}

	return info
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionSSL30:
		return "SSLv3"
	default:
		return fmt.Sprintf("0x%04x", v)
	}
}

func buildRawHTTPRequest(endpoint Endpoint, req UpstreamRequest, cookieHeader string) []byte {
	var buf bytes.Buffer
	reqPath := req.Path
	if reqPath == "" {
		reqPath = "/"
	}
	if !strings.HasPrefix(reqPath, "/") {
		reqPath = "/" + reqPath
	}

	schemeAuthority := endpoint.URL()
	incomingReferer := req.Headers.Get("Referer")
	outgoingReferer := schemeAuthority + "/"
	if strings.TrimSpace(incomingReferer) != "" {
		outgoingReferer = proxyAuthorityRegex.ReplaceAllString(incomingReferer, schemeAuthority)
	}

	buf.WriteString(req.Method)
	buf.WriteByte(' ')
	buf.WriteString(reqPath)
	buf.WriteString(" HTTP/1.1\r\n")
	buf.WriteString("Host: ")
	buf.WriteString(endpoint.Authority())
	buf.WriteString("\r\n")
	buf.WriteString("Referer: ")
	buf.WriteString(outgoingReferer)
	buf.WriteString("\r\n")
	buf.WriteString("Origin: ")
	buf.WriteString(schemeAuthority)
	buf.WriteString("\r\n")
	buf.WriteString("Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8\r\n")
	buf.WriteString("Accept-Encoding: identity\r\n")
	buf.WriteString("Connection: close\r\n")

	for key, values := range req.Headers {
		if strippedOutgoingHeaders[strings.ToLower(key)] {
			continue
		}
		for _, val := range values {
			buf.WriteString(key)
			buf.WriteString(": ")
			buf.WriteString(val)
			buf.WriteString("\r\n")
		}
	}

	if cookieHeader != "" {
		buf.WriteString("Cookie: ")
		buf.WriteString(cookieHeader)
		buf.WriteString("\r\n")
	}

	if len(req.Body) > 0 {
		ct := req.Headers.Get("Content-Type")
		if ct == "" {
			ct = "application/x-www-form-urlencoded"
		}
		buf.WriteString("Content-Type: ")
		buf.WriteString(ct)
		buf.WriteString("\r\n")
		buf.WriteString("Content-Length: ")
		buf.WriteString(strconv.Itoa(len(req.Body)))
		buf.WriteString("\r\n")
	} else if strings.EqualFold(req.Method, http.MethodPost) || strings.EqualFold(req.Method, http.MethodPut) {
		buf.WriteString("Content-Length: 0\r\n")
	}

	buf.WriteString("\r\n")
	if len(req.Body) > 0 {
		buf.Write(req.Body)
	}
	return buf.Bytes()
}

func parseRawHTTPResponse(reader *bufio.Reader, requestMethod string) (UpstreamResponse, error) {
	var headerLines []string
	for {
		line, err := readHeaderLineLenient(reader)
		if err != nil {
			// If EOF occurred right after reading headers without a final empty line, treat header end as reached.
			if len(headerLines) > 0 && isCleanOrAbruptEOF(err) {
				break
			}
			return UpstreamResponse{}, err
		}
		if line == "" {
			if len(headerLines) == 0 {
				// Skip leading blank lines if malformed server sent one.
				continue
			}
			break
		}
		headerLines = append(headerLines, line)
	}

	if len(headerLines) == 0 {
		return UpstreamResponse{}, io.EOF
	}

	statusCode, statusText := parseStatusLineLenient(headerLines[0])
	headers := make(http.Header)
	for _, line := range headerLines[1:] {
		colonIdx := strings.IndexByte(line, ':')
		if colonIdx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:colonIdx])
		val := strings.TrimSpace(line[colonIdx+1:])
		if key != "" {
			headers.Add(key, val)
		}
	}

	noBody := (statusCode >= 100 && statusCode <= 199) ||
		statusCode == http.StatusNoContent ||
		statusCode == http.StatusNotModified ||
		strings.EqualFold(requestMethod, http.MethodHead)

	var rawBody []byte
	var err error
	if !noBody {
		te := strings.ToLower(headers.Get("Transfer-Encoding"))
		clStr := strings.TrimSpace(headers.Get("Content-Length"))
		switch {
		case strings.Contains(te, "chunked"):
			rawBody, err = readChunkedBodyLenient(reader)
		case clStr != "":
			if cl, parseErr := strconv.Atoi(clStr); parseErr == nil {
				if cl <= 0 {
					rawBody = nil
				} else {
					rawBody, err = readExactLenient(reader, cl)
				}
			} else {
				rawBody, err = readUntilEOFLenient(reader)
			}
		default:
			rawBody, err = readUntilEOFLenient(reader)
		}
		if err != nil {
			return UpstreamResponse{}, err
		}
	}

	body := decompressIfNeeded(rawBody, strings.ToLower(strings.TrimSpace(headers.Get("Content-Encoding"))))
	return UpstreamResponse{
		StatusCode: statusCode,
		StatusText: statusText,
		Headers:    headers,
		Body:       body,
	}, nil
}

func readHeaderLineLenient(r *bufio.Reader) (string, error) {
	var line []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			if len(line) > 0 && isCleanOrAbruptEOF(err) {
				return strings.TrimRight(string(line), "\r"), nil
			}
			return "", err
		}
		if b == '\n' {
			return strings.TrimRight(string(line), "\r"), nil
		}
		line = append(line, b)
		if len(line) > 64*1024 {
			return "", errors.New("upstream HTTP header line exceeded 64KB")
		}
	}
}

func parseStatusLineLenient(line string) (int, string) {
	parts := strings.SplitN(strings.TrimSpace(line), " ", 3)
	if len(parts) < 2 {
		return 200, "OK"
	}
	code, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || code < 100 || code > 599 {
		return 200, "OK"
	}
	text := "OK"
	if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
		text = strings.TrimSpace(parts[2])
	}
	return code, text
}

func readExactLenient(r *bufio.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	total := 0
	for total < n {
		read, err := r.Read(buf[total:])
		total += read
		if err != nil {
			if isCleanOrAbruptEOF(err) {
				return buf[:total], nil
			}
			return nil, err
		}
	}
	return buf, nil
}

func readUntilEOFLenient(r *bufio.Reader) ([]byte, error) {
	var out bytes.Buffer
	chunk := make([]byte, 8192)
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			out.Write(chunk[:n])
		}
		if err != nil {
			if isCleanOrAbruptEOF(err) {
				return out.Bytes(), nil
			}
			return nil, err
		}
	}
}

func readChunkedBodyLenient(r *bufio.Reader) ([]byte, error) {
	var out bytes.Buffer
	for {
		sizeLine, err := readHeaderLineLenient(r)
		if err != nil {
			if isCleanOrAbruptEOF(err) {
				return out.Bytes(), nil
			}
			return nil, err
		}
		sizeLine = strings.TrimSpace(sizeLine)
		if sizeLine == "" {
			continue
		}
		hexPart := strings.TrimSpace(strings.SplitN(sizeLine, ";", 2)[0])
		chunkSize, parseErr := strconv.ParseInt(hexPart, 16, 64)
		if parseErr != nil || chunkSize < 0 {
			return out.Bytes(), nil
		}
		if chunkSize == 0 {
			for {
				trailer, tErr := readHeaderLineLenient(r)
				if tErr != nil || trailer == "" {
					break
				}
			}
			return out.Bytes(), nil
		}

		chunk, readErr := readExactLenient(r, int(chunkSize))
		out.Write(chunk)
		if readErr != nil {
			return nil, readErr
		}
		_, _ = readHeaderLineLenient(r)
	}
}

func decompressIfNeeded(body []byte, encoding string) []byte {
	if len(body) == 0 || encoding == "" {
		return body
	}
	switch {
	case strings.Contains(encoding, "gzip"):
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return body
		}
		defer gr.Close()
		decompressed, err := io.ReadAll(gr)
		if err != nil {
			return body
		}
		return decompressed
	case strings.Contains(encoding, "deflate"):
		if zr, err := zlib.NewReader(bytes.NewReader(body)); err == nil {
			defer zr.Close()
			if decompressed, rErr := io.ReadAll(zr); rErr == nil {
				return decompressed
			}
		}
		fr := flate.NewReader(bytes.NewReader(body))
		defer fr.Close()
		if decompressed, err := io.ReadAll(fr); err == nil {
			return decompressed
		}
		return body
	default:
		return body
	}
}

// Legacy embedded servers frequently close TCP right after sending HTTP bodies without sending TLS close_notify.
func isCleanOrAbruptEOF(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unexpected eof") || strings.Contains(msg, "use of closed network connection")
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
	if err == nil || isConnectionRefusedError(err) {
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
	if err == nil || isConnectionRefusedError(err) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return isImmediateResetError(err) || strings.Contains(strings.ToLower(err.Error()), "timed out")
}
