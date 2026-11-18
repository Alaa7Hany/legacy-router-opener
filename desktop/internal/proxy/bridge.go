package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alaa/legacyrouteropener/internal/bridgeui"
	"github.com/alaa/legacyrouteropener/internal/gateway"
)

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
		snapshot := s.Snapshot()
		_ = bridgeui.WriteBridge(w, snapshot.Target, snapshot.DetectedGateway, s.quitURL, snapshot)

	case strings.HasPrefix(r.URL.Path, "/_bridge/assets/") && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		bridgeui.ServeAsset(w, r, strings.TrimPrefix(r.URL.Path, "/_bridge/assets/"))

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
		gw := gateway.DetectDefaultGateway()
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
