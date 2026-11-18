package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxCachedAssets     = 128
	maxCachedAssetBytes = 512 * 1024
	maxLogEntries       = 200
	maxRequestBodyBytes = 64 * 1024 * 1024
)

var (
	strippedResponseHeaders = map[string]bool{
		"content-length":                  true,
		"content-encoding":                true,
		"set-cookie":                      true,
		"transfer-encoding":               true,
		"connection":                      true,
		"keep-alive":                      true,
		"x-frame-options":                 true,
		"content-security-policy":         true,
		"content-security-policy-report-only": true,
		"x-content-security-policy":       true,
		"x-webkit-csp":                    true,
		"cross-origin-embedder-policy":    true,
		"cross-origin-opener-policy":      true,
		"cross-origin-resource-policy":    true,
		"x-content-type-options":          true,
		"strict-transport-security":       true,
	}

	staticCacheStrippedHeaders = map[string]bool{
		"cache-control": true,
		"pragma":        true,
		"expires":       true,
	}
)

type cachedAsset struct {
	mimeType string
	body     []byte
}

// StateSnapshot represents the live control-plane state serialized for /_bridge and SSE clients.
type StateSnapshot struct {
	ProxyPort       int              `json:"proxyPort"`
	Target          string           `json:"target"`
	ResolvedTarget  string           `json:"resolvedTarget"`
	Preset          string           `json:"preset"`
	Presets         []HardwarePreset `json:"presets"`
	DetectedGateway string           `json:"detectedGateway"`
	MaxSockets      int              `json:"maxSockets"`
	CachedAssets    int              `json:"cachedAssets"`
	CookieCount     int              `json:"cookieCount"`
	TLS             TLSInfo          `json:"tls"`
	Logs            []string         `json:"logs"`
}

// Server implements both the /_bridge Web Launchpad API and the /* legacy reverse proxy.
type Server struct {
	mu              sync.RWMutex
	proxyPort       int
	target          string
	preset          HardwarePreset
	detectedGateway string
	jars            map[string]*SessionCookieJar
	staticCache     map[string]cachedAsset
	logs            []string
	sseSubscribers  map[chan struct{}]struct{}
	onStdoutLog     func(string)
	client          *Client
}

// Options configures a new proxy Server instance.
type Options struct {
	ProxyPort       int
	InitialTarget   string
	InitialPresetID string
	DetectedGateway string
	OnStdoutLog     func(string)
}

// NewServer constructs a fully initialized proxy Server.
func NewServer(opts Options) *Server {
	preset := FindPreset(opts.InitialPresetID)
	gw := strings.TrimSpace(opts.DetectedGateway)
	if gw == "" {
		gw = "192.168.1.1"
	}
	target := strings.TrimSpace(opts.InitialTarget)
	if target == "" {
		if preset.DefaultEndpoint != "" {
			target = preset.DefaultEndpoint
		} else {
			target = gw
		}
	}
	port := opts.ProxyPort
	if port <= 0 {
		port = 8080
	}

	s := &Server{
		proxyPort:       port,
		target:          target,
		preset:          preset,
		detectedGateway: gw,
		jars:            make(map[string]*SessionCookieJar),
		staticCache:     make(map[string]cachedAsset),
		sseSubscribers:  make(map[chan struct{}]struct{}),
		onStdoutLog:     opts.OnStdoutLog,
	}
	s.client = NewClient(preset.MaxConcurrentSockets, s.appendLog)
	return s
}

// SetProxyPort updates the listening port reported by the UI and URL rewriter.
func (s *Server) SetProxyPort(port int) {
	s.mu.Lock()
	s.proxyPort = port
	s.mu.Unlock()
	s.notifySubscribers()
}

// ProxyPort returns the bound listening port.
func (s *Server) ProxyPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.proxyPort
}

// UpdateTarget switches the active target endpoint and preset at runtime.
func (s *Server) UpdateTarget(rawTarget string, presetID string) error {
	trimmed := strings.TrimSpace(rawTarget)
	if trimmed == "" {
		return fmt.Errorf("target endpoint cannot be empty")
	}
	if strings.ContainsAny(trimmed, " \t\r\n") {
		return fmt.Errorf("target endpoint cannot contain whitespace")
	}

	s.mu.Lock()
	oldHost := ParseEndpoint(s.target).BareHost()
	if presetID != "" {
		s.preset = FindPreset(presetID)
	}
	s.target = trimmed
	newEndpoint := ParseEndpoint(trimmed)
	if oldHost != newEndpoint.BareHost() {
		s.staticCache = make(map[string]cachedAsset)
	}
	if jar, ok := s.jars[newEndpoint.BareHost()]; ok {
		jar.SetSeedHuawei(s.preset.SeedHuaweiCookies)
	}
	maxSock := s.preset.MaxConcurrentSockets
	presetName := s.preset.Name
	s.mu.Unlock()

	s.client.SetMaxSockets(maxSock)
	s.appendLog(fmt.Sprintf("[TARGET] %s (Preset: %s, Max Sockets: %d)", newEndpoint.URL(), presetName, maxSock))
	return nil
}

// Snapshot returns a thread-safe snapshot of current state, telemetry, and logs.
func (s *Server) Snapshot() StateSnapshot {
	s.mu.RLock()
	port := s.proxyPort
	target := s.target
	preset := s.preset
	gw := s.detectedGateway
	cacheCount := len(s.staticCache)
	logsCopy := append([]string(nil), s.logs...)
	s.mu.RUnlock()

	jar := s.activeJar()
	cookieCount := len(jar.Snapshot())
	endpoint := ParseEndpoint(target)

	return StateSnapshot{
		ProxyPort:       port,
		Target:          target,
		ResolvedTarget:  endpoint.URL(),
		Preset:          preset.ID,
		Presets:         BuiltinPresets,
		DetectedGateway: gw,
		MaxSockets:      s.client.MaxSockets(),
		CachedAssets:    cacheCount,
		CookieCount:     cookieCount,
		TLS:             s.client.LastTLSInfo(),
		Logs:            logsCopy,
	}
}

func (s *Server) activeJar() *SessionCookieJar {
	s.mu.Lock()
	defer s.mu.Unlock()

	hostKey := ParseEndpoint(s.target).BareHost()
	jar, ok := s.jars[hostKey]
	if !ok {
		jar = NewSessionCookieJar(s.preset.SeedHuaweiCookies)
		s.jars[hostKey] = jar
	}
	return jar
}

func (s *Server) appendLog(msg string) {
	line := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)

	s.mu.Lock()
	s.logs = append(s.logs, line)
	if len(s.logs) > maxLogEntries {
		s.logs = s.logs[len(s.logs)-maxLogEntries:]
	}
	onOut := s.onStdoutLog
	s.mu.Unlock()

	if onOut != nil {
		onOut(line)
	}
	s.notifySubscribers()
}

func (s *Server) subscribeSSE() chan struct{} {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.sseSubscribers[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *Server) unsubscribeSSE(ch chan struct{}) {
	s.mu.Lock()
	delete(s.sseSubscribers, ch)
	s.mu.Unlock()
}

func (s *Server) notifySubscribers() {
	s.mu.RLock()
	for ch := range s.sseSubscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	s.mu.RUnlock()
}

// ServeHTTP dispatches /_bridge control requests and reverse-proxies all other paths to the active target.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/_bridge" || strings.HasPrefix(r.URL.Path, "/_bridge/") {
		s.handleBridge(w, r)
		return
	}
	s.handleProxy(w, r)
}

func (s *Server) handleBridge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Legacy-Router-Proxy", "1")
	w.Header().Set("Cache-Control", "no-store")

	switch {
	case (r.URL.Path == "/_bridge" || r.URL.Path == "/_bridge/") && r.Method == http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, RenderBridgeHTML(s.Snapshot()))

	case r.URL.Path == "/_bridge/api/ping" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "proxy": "legacy-router-opener"})

	case r.URL.Path == "/_bridge/api/state" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, s.Snapshot())

	case r.URL.Path == "/_bridge/api/config" && r.Method == http.MethodPost:
		var req struct {
			Target string `json:"target"`
			Preset string `json:"preset"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
			return
		}
		if err := s.UpdateTarget(req.Target, req.Preset); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, s.Snapshot())

	case r.URL.Path == "/_bridge/api/reset" && r.Method == http.MethodPost:
		evicted := s.activeJar().Clear()
		s.mu.Lock()
		s.staticCache = make(map[string]cachedAsset)
		s.mu.Unlock()
		s.client.ResetState()

		for _, key := range evicted {
			w.Header().Add("Set-Cookie", fmt.Sprintf("%s=; Path=/; Max-Age=0", key))
		}
		w.Header().Set("Clear-Site-Data", `"cookies"`)
		s.appendLog("[RESET] Cleared session cookies, browser auth state, and static cache")
		writeJSON(w, http.StatusOK, s.Snapshot())

	case r.URL.Path == "/_bridge/api/detect-gateway" && r.Method == http.MethodPost:
		gw := DetectDefaultGateway()
		s.mu.Lock()
		s.detectedGateway = gw
		s.mu.Unlock()
		s.appendLog(fmt.Sprintf("[GATEWAY] Auto-detected LAN gateway: %s", gw))
		writeJSON(w, http.StatusOK, map[string]string{"gateway": gw})

	case r.URL.Path == "/_bridge/api/logs/clear" && r.Method == http.MethodPost:
		s.mu.Lock()
		s.logs = nil
		s.mu.Unlock()
		s.notifySubscribers()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	case r.URL.Path == "/_bridge/events" && r.Method == http.MethodGet:
		s.handleSSE(w, r)

	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.subscribeSSE()
	defer s.unsubscribeSSE(ch)

	send := func() bool {
		payload, err := json.Marshal(s.Snapshot())
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !send() {
		return
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			if !send() {
				return
			}
		case <-ticker.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	target := s.target
	preset := s.preset
	proxyPort := s.proxyPort
	s.mu.RUnlock()

	targetPath := r.URL.RequestURI()
	if targetPath == "" {
		targetPath = "/"
	}
	effectiveProxyHost := resolveEffectiveProxyHost(r.Host)

	isStatic := r.Method == http.MethodGet && IsStaticAsset(targetPath)
	cacheKey := fmt.Sprintf("%s|%s|%s", target, effectiveProxyHost, targetPath)
	if isStatic {
		s.mu.RLock()
		cached, hit := s.staticCache[cacheKey]
		s.mu.RUnlock()
		if hit {
			s.appendLog(fmt.Sprintf("[CACHE] %s (%dB)", r.URL.Path, len(cached.body)))
			w.Header().Set("Content-Type", cached.mimeType)
			w.Header().Set("Content-Length", strconv.Itoa(len(cached.body)))
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Header().Set("X-Legacy-Router-Proxy", "1")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(cached.body)
			return
		}
	}

	s.appendLog(fmt.Sprintf("[%s] %s", r.Method, targetPath))

	var reqBody []byte
	if r.Body != nil {
		bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
		if err != nil {
			s.appendLog(fmt.Sprintf("[ERR] Request read error: %v", err))
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		reqBody = bodyBytes
	}

	jar := s.activeJar()
	mergedCookies := jar.GetMergedCookieHeader(r.Header.Get("Cookie"))

	s.appendLog(fmt.Sprintf("[FWD] -> %s%s", target, targetPath))
	resp, usedEndpoint, err := s.client.SendRequest(r.Context(), target, preset, UpstreamRequest{
		Method:  r.Method,
		Path:    targetPath,
		Headers: r.Header,
		Body:    reqBody,
	}, mergedCookies)
	if err != nil {
		s.appendLog(fmt.Sprintf("[ERR] Router connection failed: %v", err))
		htmlErr := RenderErrorHTML(target, err.Error())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(htmlErr)))
		w.Header().Set("X-Legacy-Router-Proxy", "1")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, htmlErr)
		return
	}

	setCookies := resp.Headers.Values("Set-Cookie")
	if len(setCookies) > 0 {
		if saved := jar.SaveCookiesFromHeaders(setCookies); len(saved) > 0 {
			s.appendLog(fmt.Sprintf("[COOKIE] Saved: %s", strings.Join(saved, "; ")))
		}
	}

	computedMime := GetAccurateMimeType(targetPath, resp.Headers, resp.Body)
	finalBytes := resp.Body
	if IsTransformableText(targetPath, computedMime, resp.Body) && len(resp.Body) > 0 {
		rewritten := RewriteResponseBody(string(resp.Body), usedEndpoint.Host, proxyPort, effectiveProxyHost)
		finalBytes = []byte(rewritten)
	}

	if isStatic &&
		resp.StatusCode == http.StatusOK &&
		len(setCookies) == 0 &&
		len(finalBytes) > 0 &&
		len(finalBytes) <= maxCachedAssetBytes {
		s.mu.Lock()
		if len(s.staticCache) < maxCachedAssets {
			s.staticCache[cacheKey] = cachedAsset{mimeType: computedMime, body: finalBytes}
		}
		s.mu.Unlock()
	}

	outHeaders := w.Header()
	for headerName, values := range resp.Headers {
		lower := strings.ToLower(headerName)
		if lower == "location" {
			loc := ""
			if len(values) > 0 {
				loc = values[0]
			}
			rewrittenLoc := RewriteRedirectLocation(loc, usedEndpoint.Host, proxyPort, effectiveProxyHost, preset.RewriteSingleLabelHost)
			outHeaders.Set("Location", rewrittenLoc)
			s.appendLog(fmt.Sprintf("[REDIRECT] %s -> %s", loc, rewrittenLoc))
			continue
		}
		if strippedResponseHeaders[lower] || (isStatic && staticCacheStrippedHeaders[lower]) {
			continue
		}
		if lower == "content-type" {
			continue
		}
		for _, v := range values {
			outHeaders.Add(headerName, v)
		}
	}

	for _, sc := range setCookies {
		if cleaned := CleanSetCookieHeader(sc); cleaned != "" {
			outHeaders.Add("Set-Cookie", cleaned)
		}
	}

	if isStatic && resp.StatusCode == http.StatusOK {
		outHeaders.Set("Cache-Control", "public, max-age=3600")
	}

	outHeaders.Set("Content-Type", computedMime)
	outHeaders.Set("Content-Length", strconv.Itoa(len(finalBytes)))
	outHeaders.Set("X-Legacy-Router-Proxy", "1")

	s.appendLog(fmt.Sprintf("[%d] %s", resp.StatusCode, computedMime))
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead && len(finalBytes) > 0 {
		_, _ = w.Write(finalBytes)
	}
}

func resolveEffectiveProxyHost(hostHeader string) string {
	trimmed := strings.TrimSpace(hostHeader)
	if trimmed == "" {
		return "localhost"
	}
	host := trimmed
	if strings.HasPrefix(trimmed, "[") {
		if idx := strings.IndexByte(trimmed, ']'); idx >= 0 {
			host = trimmed[:idx+1]
		}
	} else if h, _, err := net.SplitHostPort(trimmed); err == nil {
		host = h
	}
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "[::1]":
		return host
	default:
		return "localhost"
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "json encoding error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// IsExistingProxy probes localhost:port to see if another Legacy Router Opener instance is already serving it.
func IsExistingProxy(port int) bool {
	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for _, probePath := range []string{"/_bridge/api/ping", "/html/ajaxref/updatachallangeajax.asp"} {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, probePath))
		if err != nil {
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.Header.Get("X-Legacy-Router-Proxy") == "1" {
			return true
		}
	}
	return false
}
