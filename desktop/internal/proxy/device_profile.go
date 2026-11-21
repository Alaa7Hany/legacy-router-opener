package proxy

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/alaa/legacyrouteropener/internal/upstream"
)

// deviceProfile owns every piece of state that describes the currently selected
// device: the active target, Device profile, session cookie jars, static asset
// cache and the upstream client that remembers transport fallbacks. Device
// state changes cross this one seam instead of reaching into separate owners.
type deviceProfile struct {
	mu          sync.RWMutex
	target      string
	preset      upstream.HardwarePreset
	jars        map[string]*SessionCookieJar
	staticCache map[string]cachedAsset
	client      *upstream.Client
	log         func(string)
}

// profileSnapshot is the device-owned portion of the control-plane state readout.
type profileSnapshot struct {
	Target       string
	Preset       upstream.HardwarePreset
	CachedAssets int
	CookieCount  int
	MaxSockets   int
	TLS          upstream.TLSInfo
}

func newDeviceProfile(target string, preset upstream.HardwarePreset, proxyPort, controlPort int, log func(string)) *deviceProfile {
	p := &deviceProfile{
		target:      target,
		preset:      preset,
		jars:        make(map[string]*SessionCookieJar),
		staticCache: make(map[string]cachedAsset),
		log:         log,
	}
	p.client = upstream.NewClient(preset.MaxConcurrentSockets, log)
	p.client.SetBlockedLoopbackPorts(proxyPort, controlPort)
	return p
}

// reset clears all stale sign-in state in one call: the active jar, the entire
// static cache and the upstream client's remembered transport fallbacks. It
// returns the evicted cookie names so the HTTP edge can expire them in the browser.
func (p *deviceProfile) reset() []string {
	evicted := p.activeJar().Clear()
	p.mu.Lock()
	p.staticCache = make(map[string]cachedAsset)
	p.mu.Unlock()
	p.client.ResetState()
	return evicted
}

// switchTo validates and applies a new target and Device profile, invalidating
// cached assets when the endpoint or profile changes and reseeding device cookies.
func (p *deviceProfile) switchTo(rawTarget string, presetID string) error {
	newEndpoint, err := upstream.ValidateTarget(rawTarget)
	if err != nil {
		return err
	}
	if err := p.client.CheckTargetAllowed(context.Background(), newEndpoint); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(rawTarget)

	p.mu.Lock()
	oldEndpoint := upstream.ParseEndpoint(p.target)
	oldPresetID := p.preset.ID
	if presetID != "" {
		p.preset = upstream.FindPreset(presetID)
	}
	p.target = trimmed
	if oldEndpoint.Host != newEndpoint.Host ||
		oldEndpoint.Port != newEndpoint.Port ||
		oldEndpoint.Scheme() != newEndpoint.Scheme() ||
		oldPresetID != p.preset.ID {
		p.staticCache = make(map[string]cachedAsset)
	}
	if jar, ok := p.jars[newEndpoint.BareHost()]; ok {
		jar.SetSeedHuawei(p.preset.SeedHuaweiCookies)
	}
	maxSock := p.preset.MaxConcurrentSockets
	presetName := p.preset.Name
	p.mu.Unlock()

	p.client.SetMaxSockets(maxSock)
	p.log(fmt.Sprintf("[TARGET] %s (Preset: %s, Max Sockets: %d)", newEndpoint.URL(), presetName, maxSock))
	return nil
}

func (p *deviceProfile) checkTargetAllowed(rawTarget string) error {
	ep := upstream.ParseEndpoint(rawTarget)
	return p.client.CheckTargetAllowed(context.Background(), ep)
}

func (p *deviceProfile) setBlockedLoopbackPorts(ports ...int) {
	p.client.SetBlockedLoopbackPorts(ports...)
}

// activeJar returns the cookie jar for the active target, creating it on demand.
func (p *deviceProfile) activeJar() *SessionCookieJar {
	p.mu.Lock()
	defer p.mu.Unlock()

	hostKey := upstream.ParseEndpoint(p.target).BareHost()
	jar, ok := p.jars[hostKey]
	if !ok {
		jar = NewSessionCookieJar(p.preset.SeedHuaweiCookies)
		p.jars[hostKey] = jar
	}
	return jar
}

func (p *deviceProfile) snapshot() profileSnapshot {
	p.mu.RLock()
	target := p.target
	preset := p.preset
	cacheCount := len(p.staticCache)
	p.mu.RUnlock()

	cookieCount := len(p.activeJar().Snapshot())
	return profileSnapshot{
		Target:       target,
		Preset:       preset,
		CachedAssets: cacheCount,
		CookieCount:  cookieCount,
		MaxSockets:   p.client.MaxSockets(),
		TLS:          p.client.LastTLSInfo(),
	}
}

// forwardState returns the target and Device profile used to serve device traffic.
func (p *deviceProfile) forwardState() (string, upstream.HardwarePreset) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.target, p.preset
}

func (p *deviceProfile) cachedAsset(key string) (cachedAsset, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	asset, ok := p.staticCache[key]
	return asset, ok
}

func (p *deviceProfile) cacheAsset(key string, asset cachedAsset) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.staticCache) < maxCachedAssets {
		p.staticCache[key] = asset
	}
}

func (p *deviceProfile) sendRequest(ctx context.Context, target string, preset upstream.HardwarePreset, req upstream.UpstreamRequest, cookieHeader string) (upstream.UpstreamResponse, upstream.Endpoint, error) {
	return p.client.SendRequest(ctx, target, preset, req, cookieHeader)
}
