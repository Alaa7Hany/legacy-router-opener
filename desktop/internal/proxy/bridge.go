package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alaa/legacyrouteropener/internal/bridgeui"
	"github.com/alaa/legacyrouteropener/internal/gateway"
	"github.com/alaa/legacyrouteropener/internal/upstream"
)

// IsAllowedLoopbackHost enforces the strict Host allowlist for DNS-rebinding defense.
func IsAllowedLoopbackHost(hostHeader string, port int) bool {
	if port <= 0 {
		return false
	}
	portStr := strconv.Itoa(port)
	return strings.EqualFold(hostHeader, "localhost:"+portStr) ||
		hostHeader == "127.0.0.1:"+portStr ||
		hostHeader == "[::1]:"+portStr
}

// IsAllowedLoopbackOrigin validates Origin against allowed loopback HTTP origins for the given port.
func IsAllowedLoopbackOrigin(origin string, port int) bool {
	if port <= 0 || origin == "" {
		return false
	}
	portStr := strconv.Itoa(port)
	return strings.EqualFold(origin, "http://localhost:"+portStr) ||
		strings.EqualFold(origin, "http://127.0.0.1:"+portStr) ||
		strings.EqualFold(origin, "http://[::1]:"+portStr)
}

// CanonicalLoopbackHost normalizes the case of localhost:<port> while leaving IP hosts unchanged.
func CanonicalLoopbackHost(hostHeader string) string {
	if strings.HasPrefix(strings.ToLower(hostHeader), "localhost:") {
		return "localhost:" + hostHeader[len("localhost:"):]
	}
	return hostHeader
}

// ProxyHandler serves proxied router traffic, the read-only ping endpoint, and a redirect from /_bridge to the control origin.
func (s *Server) ProxyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsAllowedLoopbackHost(r.Host, s.ProxyPort()) {
			http.NotFound(w, r)
			return
		}
		switch {
		case r.URL.Path == "/_bridge/api/ping":
			if r.Method != http.MethodGet {
				http.NotFound(w, r)
				return
			}
			s.writePing(w)
			return
		case r.URL.Path == "/_bridge" || r.URL.Path == "/_bridge/":
			if (r.Method == http.MethodGet || r.Method == http.MethodHead) && s.ControlPort() > 0 {
				w.Header().Set("Cache-Control", "no-store")
				targetURL := fmt.Sprintf("http://%s:%d/_bridge", resolveEffectiveProxyHost(r.Host), s.ControlPort())
				if r.URL.RawQuery != "" {
					targetURL += "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, targetURL, http.StatusFound)
				return
			}
			http.NotFound(w, r)
			return
		case strings.HasPrefix(r.URL.Path, "/_bridge/"):
			http.NotFound(w, r)
			return
		}
		s.handleProxy(w, r)
	})
}

// ControlHandler serves the isolated /_bridge opener UI, assets, APIs, and SSE event stream.
func (s *Server) ControlHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsAllowedLoopbackHost(r.Host, s.ControlPort()) {
			http.NotFound(w, r)
			return
		}
		s.handleControl(w, r)
	})
}

func (s *Server) writePing(w http.ResponseWriter) {
	w.Header().Set("X-Legacy-Router-Proxy", "1")
	w.Header().Set("Cache-Control", "no-store")
	if rp := s.RequestedPort(); rp > 0 {
		w.Header().Set("X-Legacy-Router-Requested-Port", strconv.Itoa(rp))
	}
	if cp := s.ControlPort(); cp > 0 {
		w.Header().Set("X-Legacy-Router-Control-Port", strconv.Itoa(cp))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "proxy": "legacy-router-opener"})
}

func (s *Server) requireControlCSRF(w http.ResponseWriter, r *http.Request, requireJSON bool) bool {
	if sfs := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")); sfs != "" && sfs != "same-origin" && sfs != "none" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-site request rejected"})
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if !IsAllowedLoopbackOrigin(origin, s.ControlPort()) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid origin"})
		return false
	}
	if requireJSON {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
			return false
		}
	}
	return true
}

func allowControlRead(w http.ResponseWriter, r *http.Request) bool {
	if sfs := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")); sfs != "" && sfs != "same-origin" && sfs != "none" {
		http.Error(w, "cross-site request rejected", http.StatusForbidden)
		return false
	}
	return true
}

func classifyTargetError(err error) string {
	if errors.Is(err, upstream.ErrSelfTargetBlocked) {
		return "address_self_blocked"
	}
	msg := err.Error()
	if strings.Contains(msg, "empty") {
		return "address_required"
	}
	if strings.Contains(msg, "whitespace") {
		return "address_spaces"
	}
	return "address_invalid"
}

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Legacy-Router-Proxy", "1")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	switch {
	case (r.URL.Path == "/_bridge" || r.URL.Path == "/_bridge/") && r.Method == http.MethodGet:
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		snapshot := s.Snapshot()
		language := bridgeui.SelectLanguage(r.URL.Query().Get("lang"), r.Header.Get("Accept-Language"))
		_ = bridgeui.WriteBridge(w, language, snapshot.Target, snapshot.DetectedGateway, s.quitURL, snapshot)

	case strings.HasPrefix(r.URL.Path, "/_bridge/assets/") && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		bridgeui.ServeAsset(w, r, strings.TrimPrefix(r.URL.Path, "/_bridge/assets/"))

	case r.URL.Path == "/_bridge/api/ping" && r.Method == http.MethodGet:
		s.writePing(w)

	case r.URL.Path == "/_bridge/api/state" && r.Method == http.MethodGet:
		if !allowControlRead(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, s.Snapshot())

	case r.URL.Path == "/_bridge/api/config" && r.Method == http.MethodPost:
		if !s.requireControlCSRF(w, r, true) {
			return
		}
		var req struct {
			Target string `json:"target"`
			Preset string `json:"preset"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
			return
		}
		// Switching and reset cross the same device-profile module seam; the
		// public UpdateTarget method is the production entry point for switches.
		if err := s.UpdateTarget(req.Target, req.Preset); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":    err.Error(),
				"errorKey": classifyTargetError(err),
			})
			return
		}
		writeJSON(w, http.StatusOK, s.Snapshot())

	case r.URL.Path == "/_bridge/api/reset" && r.Method == http.MethodPost:
		if !s.requireControlCSRF(w, r, false) {
			return
		}
		evicted := s.profile.reset()

		for _, key := range evicted {
			w.Header().Add("Set-Cookie", fmt.Sprintf("%s=; Path=/; Max-Age=0", key))
		}
		w.Header().Set("Clear-Site-Data", `"cookies"`)
		s.appendLog("[RESET] Cleared session cookies, browser auth state, and static cache")
		writeJSON(w, http.StatusOK, s.Snapshot())

	case r.URL.Path == "/_bridge/api/detect-gateway" && r.Method == http.MethodPost:
		if !s.requireControlCSRF(w, r, false) {
			return
		}
		gw := gateway.DetectDefaultGateway()
		s.mu.Lock()
		s.detectedGateway = gw
		s.mu.Unlock()
		s.appendLog(fmt.Sprintf("[GATEWAY] Auto-detected LAN gateway: %s", gw))
		writeJSON(w, http.StatusOK, map[string]string{"gateway": gw})

	case r.URL.Path == "/_bridge/api/logs/clear" && r.Method == http.MethodPost:
		if !s.requireControlCSRF(w, r, false) {
			return
		}
		s.mu.Lock()
		s.logs = nil
		s.mu.Unlock()
		s.notifySubscribers()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	case r.URL.Path == "/_bridge/events" && r.Method == http.MethodGet:
		if !allowControlRead(w, r) {
			return
		}
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

	if s.watchOpenerTab != nil && r.URL.Query().Get("opener") == "1" &&
		r.Header.Get("Sec-Fetch-Site") == "same-origin" {
		endTab := s.watchOpenerTab()
		defer endTab()
	}

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
		case <-s.stopping:
			return
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
