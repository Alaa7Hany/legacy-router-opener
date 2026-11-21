package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alaa/legacyrouteropener/internal/proxy"
	"github.com/alaa/legacyrouteropener/internal/upstream"
)

func TestListenWithFallbackAndExistingProxy(t *testing.T) {
	// 1. Occupy a port with a non-proxy listener and ensure fallback picks next free port.
	occupant, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen occupant: %v", err)
	}
	defer occupant.Close()

	basePort := occupant.Addr().(*net.TCPAddr).Port
	listeners, boundPort, alreadyRunning, _, err := listenWithFallback(basePort)
	if err != nil {
		t.Fatalf("listenWithFallback failed: %v", err)
	}
	closeListeners(listeners)
	if alreadyRunning {
		t.Fatalf("expected alreadyRunning=false for non-proxy occupant")
	}
	if boundPort <= basePort {
		t.Fatalf("expected fallback port > %d, got %d", basePort, boundPort)
	}

	// 2. Start a real proxy server and verify listenWithFallback detects it as alreadyRunning with its controlPort.
	// Ports are bound before construction so the advertised identity is immutable.
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyPort := ln.Addr().(*net.TCPAddr).Port
	srv := proxy.NewServer(proxy.Options{
		InitialTarget: "192.168.1.1",
		ProxyPort:     proxyPort,
		ControlPort:   proxyPort + controlPortOffset,
		RequestedPort: proxyPort,
	})
	ts := httptest.NewUnstartedServer(srv.ProxyHandler())
	_ = ts.Listener.Close()
	ts.Listener = ln
	ts.Start()
	defer ts.Close()

	listeners2, boundPort2, alreadyRunning2, controlPort2, err := listenWithFallback(proxyPort)
	closeListeners(listeners2)
	if err != nil {
		t.Fatalf("listenWithFallback(existing) failed: %v", err)
	}
	if !alreadyRunning2 || boundPort2 != proxyPort || controlPort2 != proxyPort+controlPortOffset {
		t.Fatalf("expected alreadyRunning=true on port %d (control %d), got alreadyRunning=%v port=%d control=%d",
			proxyPort, proxyPort+controlPortOffset, alreadyRunning2, boundPort2, controlPort2)
	}
}

func TestExistingProxyReusedAfterPreferredPortBecomesFree(t *testing.T) {
	preferred, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer preferred.Close()
	preferredPort := preferred.Addr().(*net.TCPAddr).Port
	held, fallbackPort, existing, _, err := listenWithFallback(preferredPort)
	if err != nil || existing || fallbackPort == preferredPort {
		t.Fatalf("reserve fallback port = (%d, %t, %v)", fallbackPort, existing, err)
	}
	defer closeListeners(held)
	if err := preferred.Close(); err != nil {
		t.Fatal(err)
	}

	bridge := proxy.NewServer(proxy.Options{
		ProxyPort:     fallbackPort,
		ControlPort:   fallbackPort + controlPortOffset,
		RequestedPort: preferredPort,
	})
	server := &http.Server{Handler: bridge.ProxyHandler()}
	go server.Serve(held[0])
	defer server.Close()

	listeners, port, existing, controlPort, err := listenWithFallback(preferredPort)
	closeListeners(listeners)
	if err != nil || !existing || port != fallbackPort || controlPort != fallbackPort+controlPortOffset {
		t.Fatalf("fallback instance discovery = (%d, %t, %d, %v), want existing on %d (control %d)",
			port, existing, controlPort, err, fallbackPort, fallbackPort+controlPortOffset)
	}
}

func TestLaunchProbesIdentityHeadersAndReusesExistingBridge(t *testing.T) {
	preferred, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	preferredPort := preferred.Addr().(*net.TCPAddr).Port

	// A manual launch on an occupied preferred port binds the next free port and
	// advertises the original request on its ping endpoint.
	firstCtx, cancelFirst := context.WithCancel(t.Context())
	defer cancelFirst()
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- run(firstCtx, launchOptions{
			port: preferredPort, target: "192.0.2.1", noBrowser: true,
		})
	}()

	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	var ping *http.Response
	fallbackPort := 0
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && fallbackPort == 0 {
		for port := preferredPort + 1; port <= min(preferredPort+maxPortAttempts-1, 65535); port++ {
			resp, err := client.Get(fmt.Sprintf("http://localhost:%d/_bridge/api/ping", port))
			if err == nil && resp.StatusCode == http.StatusOK &&
				resp.Header.Get("X-Legacy-Router-Proxy") == "1" &&
				resp.Header.Get(requestedPortField) == strconv.Itoa(preferredPort) {
				ping = resp
				fallbackPort = port
				break
			}
			if resp != nil {
				_ = resp.Body.Close()
			}
		}
		if fallbackPort == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if ping == nil {
		t.Fatalf("first bridge did not advertise the preferred port on any fallback port after %d", preferredPort)
	}
	defer ping.Body.Close()
	if got := ping.Header.Get("X-Legacy-Router-Proxy"); got != "1" {
		t.Fatalf("X-Legacy-Router-Proxy = %q, want 1", got)
	}
	if got := ping.Header.Get(requestedPortField); got != strconv.Itoa(preferredPort) {
		t.Fatalf("%s = %q, want %d", requestedPortField, got, preferredPort)
	}
	controlPort := ping.Header.Get(controlPortField)
	if controlPort == "" || controlPort == strconv.Itoa(preferredPort) || controlPort == strconv.Itoa(fallbackPort) {
		t.Fatalf("%s = %q, want a distinct control port", controlPortField, controlPort)
	}
	// The two origins stay separate: the Opener lives on the control port only.
	proxyLaunchpad, err := client.Get(fmt.Sprintf("http://localhost:%d/_bridge", fallbackPort))
	if err != nil {
		t.Fatal(err)
	}
	_ = proxyLaunchpad.Body.Close()
	if proxyLaunchpad.StatusCode != http.StatusFound ||
		proxyLaunchpad.Header.Get("Location") != "http://localhost:"+controlPort+"/_bridge" {
		t.Fatalf("proxy origin /_bridge = (%d, %q), want redirect to control origin",
			proxyLaunchpad.StatusCode, proxyLaunchpad.Header.Get("Location"))
	}

	// Free the preferred port; a second launch must now detect the fallback
	// instance through its ping headers instead of claiming the port again.
	if err := preferred.Close(); err != nil {
		t.Fatal(err)
	}
	secondCtx, cancelSecond := context.WithCancel(t.Context())
	defer cancelSecond()
	if err := run(secondCtx, launchOptions{port: preferredPort, target: "192.0.2.1", noBrowser: true}); err != nil {
		t.Fatalf("second launch should reuse the running bridge: %v", err)
	}
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", preferredPort), 200*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatalf("reused launch claimed preferred port %d", preferredPort)
	}
	reusedPing, err := client.Get(fmt.Sprintf("http://localhost:%d/_bridge/api/ping", fallbackPort))
	if err != nil {
		t.Fatalf("existing bridge stopped responding after reuse: %v", err)
	}
	defer reusedPing.Body.Close()
	if reusedPing.Header.Get("X-Legacy-Router-Proxy") != "1" {
		t.Fatal("existing bridge lost its identity after reuse")
	}

	cancelFirst()
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first bridge stopped with error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first bridge did not stop cleanly")
	}
}

func TestListenRejectsIPv6ShadowedPort(t *testing.T) {
	v6, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	defer v6.Close()

	shadowedPort := v6.Addr().(*net.TCPAddr).Port
	// Confirm 127.0.0.1:<shadowedPort> is free so this specifically tests IPv6 shadowing!
	if probe4, err4 := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", shadowedPort)); err4 != nil {
		t.Skipf("127.0.0.1:%d not free on host: %v", shadowedPort, err4)
	} else {
		_ = probe4.Close()
	}

	listeners, boundPort, existing, _, err := listenWithFallback(shadowedPort)
	closeListeners(listeners)
	if err != nil {
		t.Fatalf("listenWithFallback failed: %v", err)
	}
	if existing || boundPort == shadowedPort {
		t.Fatalf("expected listenWithFallback to skip IPv6-shadowed port %d, got existing=%v port=%d", shadowedPort, existing, boundPort)
	}

	if shadowedPort > controlPortOffset {
		ctrlLns, ctrlPort, err := listenControlWithFallback(shadowedPort - controlPortOffset)
		closeListeners(ctrlLns)
		if err != nil {
			t.Fatalf("listenControlWithFallback failed: %v", err)
		}
		if ctrlPort == shadowedPort {
			t.Fatalf("expected listenControlWithFallback to skip IPv6-shadowed port %d", shadowedPort)
		}
	}
}

func TestStartupRejectsInvalidAndSelfTargets(t *testing.T) {
	if _, _, err := startApp(launchOptions{target: "javascript:alert(1)", noBrowser: true}); err == nil {
		t.Fatal("expected startApp to reject javascript: target")
	}
	if _, _, err := startApp(launchOptions{target: "192.168.1.1:70000", noBrowser: true}); err == nil {
		t.Fatal("expected startApp to reject out-of-range port")
	}

	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freePort := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	if _, _, err := startApp(launchOptions{port: freePort, target: fmt.Sprintf("127.0.0.1:%d", freePort), noBrowser: true}); err == nil {
		t.Fatal("expected startApp to reject self-targeting proxy port")
	}
}

func TestShouldShowErrorDialog(t *testing.T) {
	if shouldShowErrorDialog(true, false) {
		t.Fatal("noBrowser=true must suppress modal dialog")
	}
	if shouldShowErrorDialog(false, true) {
		t.Fatal("attached console must suppress modal dialog")
	}
}

func TestRunReportsWhenNoPortIsAvailable(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:65535")
	if err != nil {
		t.Skipf("cannot reserve the final port for this test: %v", err)
	}
	defer listener.Close()

	err = run(t.Context(), launchOptions{port: 65535, noBrowser: true})
	if err == nil || !strings.Contains(err.Error(), "Could not start Router Page Opener: no available proxy port") {
		t.Fatalf("unexpected startup result: %v", err)
	}
}

func TestRSA1024And3DESWithGoDebugDirectives(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("rsa.GenerateKey(1024): %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "legacy-1024-router"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	cert := tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
	}

	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer rawLn.Close()

	tlsLn := tls.NewListener(rawLn, &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS10,
		MaxVersion:   tls.VersionTLS11,
		CipherSuites: []uint16{tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA},
	})
	defer tlsLn.Close()

	go func() {
		for {
			c, err := tlsLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = http.ReadRequest(bufio.NewReader(conn))
				_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 10\r\n\r\nrsa1024-ok")
			}(c)
		}
	}()

	client := upstream.NewClient(4, nil)
	resp, _, err := client.SendRequest(
		context.Background(),
		"https://"+rawLn.Addr().String(),
		upstream.FindPreset("huawei-zte"),
		upstream.UpstreamRequest{Method: http.MethodGet, Path: "/"},
		"",
	)
	if err != nil {
		t.Fatalf("1024-bit RSA + 3DES TLS 1.1 request failed: %v", err)
	}
	if string(resp.Body) != "rsa1024-ok" {
		t.Fatalf("Body = %q, want rsa1024-ok", string(resp.Body))
	}
	info := client.LastTLSInfo()
	if !strings.Contains(info.PeerKeyBits, "1024-bit RSA") {
		t.Fatalf("expected 1024-bit RSA telemetry, got %+v", info)
	}
}

func TestVersionFlagAndFormatting(t *testing.T) {
	if got := formatVersion(version); got != "legacy-router-opener "+version {
		t.Fatalf("formatVersion(default %q) = %q", version, got)
	}
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"", "legacy-router-opener dev"},
		{"   ", "legacy-router-opener dev"},
		{"dev", "legacy-router-opener dev"},
		{"0.9.0", "legacy-router-opener 0.9.0"},
		{" 1.0.0-rc.1 ", "legacy-router-opener 1.0.0-rc.1"},
	} {
		if got := formatVersion(tc.in); got != tc.want {
			t.Fatalf("formatVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	for _, flagArg := range []string{"-version", "--version"} {
		opts, err := parseLaunchOptionsArgs([]string{flagArg}, nil, io.Discard)
		if err != nil {
			t.Fatalf("parseLaunchOptionsArgs(%q) failed: %v", flagArg, err)
		}
		if !opts.showVersion {
			t.Fatalf("parseLaunchOptionsArgs(%q) showVersion = false, want true", flagArg)
		}
	}

	opts, err := parseLaunchOptionsArgs([]string{"-port", "9090", "192.168.100.1"}, nil, io.Discard)
	if err != nil {
		t.Fatalf("parseLaunchOptionsArgs(normal) failed: %v", err)
	}
	if opts.showVersion || opts.port != 9090 || opts.target != "192.168.100.1" {
		t.Fatalf("unexpected parsed options: %+v", opts)
	}
}
