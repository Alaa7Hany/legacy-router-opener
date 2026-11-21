package proxy

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alaa/legacyrouteropener/internal/upstream"
)

const maxLogEntries = 200

// StateSnapshot represents the live control-plane state serialized for /_bridge and SSE clients.
type StateSnapshot struct {
	ProxyPort       int                       `json:"proxyPort"`
	Target          string                    `json:"target"`
	ResolvedTarget  string                    `json:"resolvedTarget"`
	Preset          string                    `json:"preset"`
	Presets         []upstream.HardwarePreset `json:"presets"`
	DetectedGateway string                    `json:"detectedGateway"`
	MaxSockets      int                       `json:"maxSockets"`
	CachedAssets    int                       `json:"cachedAssets"`
	CookieCount     int                       `json:"cookieCount"`
	TLS             upstream.TLSInfo          `json:"tls"`
	Logs            []string                  `json:"logs"`
}

// Server implements the separate /_bridge control-plane handler and /* legacy reverse-proxy handler.
type Server struct {
	mu              sync.RWMutex
	proxyPort       int
	controlPort     int
	requestedPort   int
	profile         *deviceProfile
	detectedGateway string
	logs            []string
	sseSubscribers  map[chan struct{}]struct{}
	onStdoutLog     func(string)
	quitURL         string
	stopping        <-chan struct{}
}

// Options configures a new proxy Server instance.
type Options struct {
	ProxyPort       int
	ControlPort     int
	RequestedPort   int
	InitialTarget   string
	InitialPresetID string
	DetectedGateway string
	OnStdoutLog     func(string)
	QuitURL         string
	Stopping        <-chan struct{}
}

// NewServer constructs a fully initialized proxy Server.
func NewServer(opts Options) *Server {
	preset := upstream.FindPreset(opts.InitialPresetID)
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
		controlPort:     opts.ControlPort,
		requestedPort:   opts.RequestedPort,
		detectedGateway: gw,
		sseSubscribers:  make(map[chan struct{}]struct{}),
		onStdoutLog:     opts.OnStdoutLog,
		quitURL:         opts.QuitURL,
		stopping:        opts.Stopping,
	}
	s.profile = newDeviceProfile(target, preset, port, opts.ControlPort, s.appendLog)
	return s
}

// ProxyPort returns the bound proxy listening port.
func (s *Server) ProxyPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.proxyPort
}

// ControlPort returns the bound control/opener listening port.
func (s *Server) ControlPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.controlPort
}

// RequestedPort returns the requested proxy port reported on ping responses.
func (s *Server) RequestedPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.requestedPort
}

// UpdateTarget switches the active target endpoint and preset at runtime.
func (s *Server) UpdateTarget(rawTarget string, presetID string) error {
	return s.profile.switchTo(rawTarget, presetID)
}

// CheckTargetAllowed reports whether rawTarget passes blocked loopback port checks.
func (s *Server) CheckTargetAllowed(rawTarget string) error {
	return s.profile.checkTargetAllowed(rawTarget)
}

// Snapshot returns a thread-safe snapshot of current state, telemetry, and logs.
func (s *Server) Snapshot() StateSnapshot {
	s.mu.RLock()
	port := s.proxyPort
	gw := s.detectedGateway
	logsCopy := append([]string(nil), s.logs...)
	s.mu.RUnlock()

	profile := s.profile.snapshot()
	endpoint := upstream.ParseEndpoint(profile.Target)

	return StateSnapshot{
		ProxyPort:       port,
		Target:          profile.Target,
		ResolvedTarget:  endpoint.URL(),
		Preset:          profile.Preset.ID,
		Presets:         upstream.BuiltinPresets,
		DetectedGateway: gw,
		MaxSockets:      profile.MaxSockets,
		CachedAssets:    profile.CachedAssets,
		CookieCount:     profile.CookieCount,
		TLS:             profile.TLS,
		Logs:            logsCopy,
	}
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
