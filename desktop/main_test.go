package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alaa/legacyrouteropener/internal/proxy"
)

func TestListenWithFallbackAndExistingProxy(t *testing.T) {
	// 1. Occupy a port with a non-proxy listener and ensure fallback picks next free port.
	occupant, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen occupant: %v", err)
	}
	defer occupant.Close()

	basePort := occupant.Addr().(*net.TCPAddr).Port
	listeners, boundPort, alreadyRunning, err := listenWithFallback(basePort)
	if err != nil {
		t.Fatalf("listenWithFallback failed: %v", err)
	}
	for _, ln := range listeners {
		_ = ln.Close()
	}
	if alreadyRunning {
		t.Fatalf("expected alreadyRunning=false for non-proxy occupant")
	}
	if boundPort <= basePort {
		t.Fatalf("expected fallback port > %d, got %d", basePort, boundPort)
	}

	// 2. Start a real proxy server and verify listenWithFallback detects it as alreadyRunning.
	srv := proxy.NewServer(proxy.Options{InitialTarget: "192.168.1.1"})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	proxyPort := ts.Listener.Addr().(*net.TCPAddr).Port
	listeners2, boundPort2, alreadyRunning2, err := listenWithFallback(proxyPort)
	if err != nil {
		t.Fatalf("listenWithFallback(existing) failed: %v", err)
	}
	for _, ln := range listeners2 {
		_ = ln.Close()
	}
	if !alreadyRunning2 || boundPort2 != proxyPort {
		t.Fatalf("expected alreadyRunning=true on port %d, got alreadyRunning=%v port=%d", proxyPort, alreadyRunning2, boundPort2)
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

	client := proxy.NewClient(4, nil)
	resp, _, err := client.SendRequest(
		context.Background(),
		"https://"+rawLn.Addr().String(),
		proxy.FindPreset("huawei-zte"),
		proxy.UpstreamRequest{Method: http.MethodGet, Path: "/"},
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
