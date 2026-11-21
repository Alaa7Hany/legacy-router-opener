package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alaa/legacyrouteropener/internal/proxy"
)

const (
	maxPortAttempts        = 10
	maxControlPortAttempts = 20
	controlPortOffset      = 100
	pingPath               = "/_bridge/api/ping"
	requestedPortField     = "X-Legacy-Router-Requested-Port"
	controlPortField       = "X-Legacy-Router-Control-Port"

	// openerHost never varies; it is the canonical host named by the one
	// normalizer the control plane and this lifetime module share.
	openerHost = proxy.CanonicalOpenerHost
)

// bridgeIdentity is the Bridge's immutable local address, resolved before any
// handler is constructed so no caller has to patch ports afterwards.
type bridgeIdentity struct {
	proxyPort     int
	controlPort   int
	requestedPort int
}

// bridgeProcess owns port fallback, both local origins, instance reuse and
// opener registration. Its identity is fixed once construction succeeds.
type bridgeProcess struct {
	identity         bridgeIdentity
	proxyListeners   []net.Listener
	controlListeners []net.Listener
	reused           bool
	tabs             *tabLifetime

	// stopCh is the Bridge's single stop signal. A normal quit sends nil; a
	// failed server sends the error. stopOnce makes the first reason win.
	stopCh   chan error
	stopOnce sync.Once

	// stopping terminates both event streams so shutdown can finish.
	stopping     chan struct{}
	stoppingOnce sync.Once
}

// newBridgeProcess binds both local origins under the startup lock, probes for
// an already-running Bridge and returns the identity the caller builds the
// control plane from. When an existing Bridge answers, reused is true and this
// process holds no listeners of its own.
func newBridgeProcess(requestedPort int, startupTimeout time.Duration) (*bridgeProcess, error) {
	unlock, err := lockStartup(requestedPort)
	if err != nil {
		return nil, err
	}
	defer unlock()

	listeners, port, existing, existingControlPort, err := listenWithFallback(requestedPort)
	if err != nil {
		return nil, err
	}
	process := &bridgeProcess{
		identity: bridgeIdentity{
			proxyPort:     port,
			requestedPort: requestedPort,
		},
		stopCh:   make(chan error, 1),
		stopping: make(chan struct{}),
	}
	if existing {
		openerPort := existingControlPort
		if openerPort <= 0 {
			openerPort = port
		}
		process.reused = true
		process.identity.controlPort = openerPort
		return process, nil
	}

	controlListeners, controlPort, err := listenControlWithFallback(port)
	if err != nil {
		closeListeners(listeners)
		return nil, err
	}
	process.proxyListeners = listeners
	process.controlListeners = controlListeners
	process.identity.controlPort = controlPort
	process.tabs = newTabLifetime(startupTimeout, func() { process.stop(nil) })
	return process, nil
}

// stop is the only way to request shutdown. The first caller wins: a user quit
// and a last-tab close send no error, a failed server sends its error.
func (p *bridgeProcess) stop(err error) {
	p.stopOnce.Do(func() { p.stopCh <- err })
}

// stopSignal is the single channel the launch call selects on.
func (p *bridgeProcess) stopSignal() <-chan error {
	return p.stopCh
}

// streamStop is closed when shutdown begins, ending both event streams.
func (p *bridgeProcess) streamStop() <-chan struct{} {
	return p.stopping
}

// proxyPort, controlPort and requestedPort expose the immutable identity
// without letting callers reach into the identity value itself.
func (p *bridgeProcess) proxyPort() int { return p.identity.proxyPort }

func (p *bridgeProcess) controlPort() int { return p.identity.controlPort }

func (p *bridgeProcess) requestedPort() int { return p.identity.requestedPort }

func (p *bridgeProcess) reusedBridge() bool { return p.reused }

// listeners returns the proxy and control bindings this process owns.
func (p *bridgeProcess) listeners() (proxyBindings, controlBindings []net.Listener) {
	return p.proxyListeners, p.controlListeners
}

// openerURL is the canonical Opener address advertised on this Bridge.
func (p *bridgeProcess) openerURL() string {
	return proxy.OpenerURL(openerHost, p.identity.controlPort, "")
}

// launchURL is the address handed to the browser on startup.
func (p *bridgeProcess) launchURL(openRouter bool) string {
	if openRouter {
		return fmt.Sprintf("http://%s:%d/", openerHost, p.identity.proxyPort)
	}
	return p.openerURL()
}

// proxyHandler injects the requested port on the proxy origin's ping response
// so a later launch can match this instance even after its preferred port frees.
func (p *bridgeProcess) proxyHandler(handler http.Handler) http.Handler {
	port := strconv.Itoa(p.identity.requestedPort)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pingPath {
			w.Header().Set(requestedPortField, port)
		}
		handler.ServeHTTP(w, r)
	})
}

// registerOpener records an attached Opener stream and returns its cleanup.
func (p *bridgeProcess) registerOpener() func() {
	if p.tabs == nil {
		return func() {}
	}
	return p.tabs.connect()
}

// endStreams releases tab state and ends both event streams so a server
// shutdown can finish waiting on active connections.
func (p *bridgeProcess) endStreams() {
	if p.tabs != nil {
		p.tabs.close()
	}
	p.stoppingOnce.Do(func() { close(p.stopping) })
}

// closeBoundListeners cleans up any listener a server did not close itself.
// http.Server.Shutdown closes the listeners handed to Serve, so this runs only
// after the servers are gone (or when shutdown never started them).
func (p *bridgeProcess) closeBoundListeners() {
	closeListeners(p.proxyListeners)
	closeListeners(p.controlListeners)
}

// close is the full cleanup for paths where no server was ever started.
func (p *bridgeProcess) close() {
	p.endStreams()
	p.closeBoundListeners()
}

func validProxyPort(port int) int {
	if port < 1 || port > 65535 {
		return 8080
	}
	return port
}

func listenWithFallback(startPort int) ([]net.Listener, int, bool, int, error) {
	startPort = validProxyPort(startPort)
	lastPort := min(startPort+maxPortAttempts-1, 65535)
	var selected []net.Listener
	var boundPort int

	for port := startPort; port <= lastPort; port++ {
		if selected == nil {
			lns, bound, err := listenDualLoopback(port)
			if err == nil {
				selected, boundPort = lns, bound
				continue
			}
		} else {
			ln, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
			if err == nil {
				_ = ln.Close()
				continue
			}
		}

		// A previous launch may have used a fallback port even if the preferred port is free now.
		if existing, controlPort := isExistingProxy(port, startPort, selected == nil); existing {
			closeListeners(selected)
			return nil, port, true, controlPort, nil
		}
	}

	if len(selected) == 0 {
		return nil, 0, false, 0, fmt.Errorf("no available proxy port in range %d–%d", startPort, lastPort)
	}
	return selected, boundPort, false, 0, nil
}

func listenControlWithFallback(proxyPort int) ([]net.Listener, int, error) {
	preferred := proxyPort + controlPortOffset
	if preferred <= 65535 {
		lastPort := min(preferred+maxControlPortAttempts-1, 65535)
		for port := preferred; port <= lastPort; port++ {
			if lns, bound, err := listenDualLoopback(port); err == nil {
				return lns, bound, nil
			}
		}
	}
	lns, bound, err := listenDualLoopback(0)
	if err != nil {
		return nil, 0, fmt.Errorf("no available control port: %w", err)
	}
	return lns, bound, nil
}

func listenDualLoopback(port int) ([]net.Listener, int, error) {
	if port == 0 {
		var lastErr error
		for attempt := 0; attempt < 10; attempt++ {
			ln4, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				return nil, 0, err
			}
			bound := ln4.Addr().(*net.TCPAddr).Port
			ln6, err6 := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", bound))
			if err6 == nil {
				return []net.Listener{ln4, ln6}, bound, nil
			}
			if isIPv6Unavailable(err6) {
				return []net.Listener{ln4}, bound, nil
			}
			_ = ln4.Close()
			lastErr = err6
		}
		return nil, 0, lastErr
	}

	ln4, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, 0, err
	}
	ln6, err6 := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", port))
	if err6 == nil {
		return []net.Listener{ln4, ln6}, port, nil
	}
	if isIPv6Unavailable(err6) {
		return []net.Listener{ln4}, port, nil
	}
	_ = ln4.Close()
	return nil, 0, err6
}

func isIPv6Unavailable(err error) bool {
	if err == nil {
		return false
	}
	// Include Windows Winsock codes alongside POSIX errnos:
	// WSAEPROTONOSUPPORT (10043), WSAEAFNOSUPPORT (10047), WSAEADDRNOTAVAIL (10049).
	if errors.Is(err, syscall.EADDRNOTAVAIL) ||
		errors.Is(err, syscall.EAFNOSUPPORT) ||
		errors.Is(err, syscall.EPROTONOSUPPORT) ||
		errors.Is(err, syscall.Errno(10043)) ||
		errors.Is(err, syscall.Errno(10047)) ||
		errors.Is(err, syscall.Errno(10049)) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "address family not supported") ||
		strings.Contains(msg, "cannot assign requested address") ||
		strings.Contains(msg, "requested address is not valid") ||
		strings.Contains(msg, "protocol not supported")
}

func closeListeners(listeners []net.Listener) {
	for _, ln := range listeners {
		_ = ln.Close()
	}
}

func isExistingProxy(port, requestedPort int, allowLegacy bool) (bool, int) {
	client := &http.Client{
		Timeout: 350 * time.Millisecond,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	probe := func(path string) (bool, string, int) {
		for _, host := range []string{fmt.Sprintf("127.0.0.1:%d", port), fmt.Sprintf("[::1]:%d", port)} {
			resp, err := client.Get("http://" + host + path)
			if err != nil {
				continue
			}
			_ = resp.Body.Close()
			cp, _ := strconv.Atoi(resp.Header.Get(controlPortField))
			if cp < 1 || cp > 65535 {
				cp = 0
			}
			return resp.Header.Get("X-Legacy-Router-Proxy") == "1", resp.Header.Get(requestedPortField), cp
		}
		return false, "", 0
	}
	if found, originalPort, controlPort := probe(pingPath); found {
		matched := port == requestedPort || originalPort == strconv.Itoa(requestedPort) ||
			(allowLegacy && originalPort == "")
		return matched, controlPort
	}
	if port == requestedPort {
		// Older Node.js launches identify themselves on the router polling endpoint.
		found, _, _ := probe("/html/ajaxref/updatachallangeajax.asp")
		return found, 0
	}
	return false, 0
}
