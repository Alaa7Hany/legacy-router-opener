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

// Server implements both the /_bridge Web Launchpad API and the /* legacy reverse proxy.
type Server struct {
	mu              sync.RWMutex
	proxyPort       int
	target          string
	preset          upstream.HardwarePreset
	detectedGateway string
	jars            map[string]*SessionCookieJar
	staticCache     map[string]cachedAsset
	logs            []string
	sseSubscribers  map[chan struct{}]struct{}
	onStdoutLog     func(string)
	client          *upstream.Client
	quitURL         string
	stopping        <-chan struct{}
	watchOpenerTab  func() func()
}

// Options configures a new proxy Server instance.
type Options struct {
	ProxyPort       int
	InitialTarget   string
	InitialPresetID string
	DetectedGateway string
	OnStdoutLog     func(string)
	QuitURL         string
	Stopping        <-chan struct{}
	WatchOpenerTab  func() func()
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
		target:          target,
		preset:          preset,
		detectedGateway: gw,
		jars:            make(map[string]*SessionCookieJar),
		staticCache:     make(map[string]cachedAsset),
		sseSubscribers:  make(map[chan struct{}]struct{}),
		onStdoutLog:     opts.OnStdoutLog,
		quitURL:         opts.QuitURL,
		stopping:        opts.Stopping,
		watchOpenerTab:  opts.WatchOpenerTab,
	}
	s.client = upstream.NewClient(preset.MaxConcurrentSockets, s.appendLog)
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
	oldHost := upstream.ParseEndpoint(s.target).BareHost()
	if presetID != "" {
		s.preset = upstream.FindPreset(presetID)
	}
	s.target = trimmed
	newEndpoint := upstream.ParseEndpoint(trimmed)
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
	endpoint := upstream.ParseEndpoint(target)

	return StateSnapshot{
		ProxyPort:       port,
		Target:          target,
		ResolvedTarget:  endpoint.URL(),
		Preset:          preset.ID,
		Presets:         upstream.BuiltinPresets,
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

	hostKey := upstream.ParseEndpoint(s.target).BareHost()
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
