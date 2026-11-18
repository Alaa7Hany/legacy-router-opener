package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHost string
		wantPort int
		wantTLS  bool
		wantExp  bool
		wantAuth string
		wantDial string
	}{
		{
			name:     "empty defaults to 192.168.1.1:443",
			input:    "",
			wantHost: "192.168.1.1",
			wantPort: 443,
			wantTLS:  true,
			wantExp:  false,
			wantAuth: "192.168.1.1",
			wantDial: "192.168.1.1:443",
		},
		{
			name:     "bare IPv4 defaults to HTTPS 443 implicit",
			input:    "192.168.1.1",
			wantHost: "192.168.1.1",
			wantPort: 443,
			wantTLS:  true,
			wantExp:  false,
			wantAuth: "192.168.1.1",
			wantDial: "192.168.1.1:443",
		},
		{
			name:     "explicit http scheme defaults to port 80",
			input:    "http://192.168.1.1",
			wantHost: "192.168.1.1",
			wantPort: 80,
			wantTLS:  false,
			wantExp:  true,
			wantAuth: "192.168.1.1",
			wantDial: "192.168.1.1:80",
		},
		{
			name:     "explicit https scheme on port 80 preserves TLS",
			input:    "https://192.168.1.1:80/login",
			wantHost: "192.168.1.1",
			wantPort: 80,
			wantTLS:  true,
			wantExp:  true,
			wantAuth: "192.168.1.1:80",
			wantDial: "192.168.1.1:80",
		},
		{
			name:     "custom port 8443 assumes TLS",
			input:    "192.168.0.120:8443/index.html",
			wantHost: "192.168.0.120",
			wantPort: 8443,
			wantTLS:  true,
			wantExp:  true,
			wantAuth: "192.168.0.120:8443",
			wantDial: "192.168.0.120:8443",
		},
		{
			name:     "port 80 without scheme disables TLS",
			input:    "10.0.0.1:80",
			wantHost: "10.0.0.1",
			wantPort: 80,
			wantTLS:  false,
			wantExp:  true,
			wantAuth: "10.0.0.1",
			wantDial: "10.0.0.1:80",
		},
		{
			name:     "bracketed IPv6 with custom port",
			input:    "[fe80::1]:8443",
			wantHost: "[fe80::1]",
			wantPort: 8443,
			wantTLS:  true,
			wantExp:  true,
			wantAuth: "[fe80::1]:8443",
			wantDial: "[fe80::1]:8443",
		},
		{
			name:     "bare IPv6 address wraps in brackets",
			input:    "::1",
			wantHost: "[::1]",
			wantPort: 443,
			wantTLS:  true,
			wantExp:  false,
			wantAuth: "[::1]",
			wantDial: "[::1]:443",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ep := ParseEndpoint(tc.input)
			if ep.Host != tc.wantHost || ep.Port != tc.wantPort || ep.UseTLS != tc.wantTLS || ep.ExplicitPortOrScheme != tc.wantExp {
				t.Fatalf("ParseEndpoint(%q) = %+v, want host=%q port=%d tls=%v exp=%v",
					tc.input, ep, tc.wantHost, tc.wantPort, tc.wantTLS, tc.wantExp)
			}
			if ep.Authority() != tc.wantAuth {
				t.Fatalf("Authority() = %q, want %q", ep.Authority(), tc.wantAuth)
			}
			if ep.DialAddress() != tc.wantDial {
				t.Fatalf("DialAddress() = %q, want %q", ep.DialAddress(), tc.wantDial)
			}
		})
	}
}

func TestGatewayParsers(t *testing.T) {
	winRoute := `
IPv4 Route Table
===========================================================================
Active Routes:
Network Destination        Netmask          Gateway       Interface  Metric
          0.0.0.0          0.0.0.0         On-link       10.10.0.2     45
          0.0.0.0          0.0.0.0      192.168.8.1    192.168.8.2     35
          0.0.0.0          0.0.0.0      192.168.1.1    192.168.1.5     25
===========================================================================`
	if got := ParseWindowsRoutePrint(winRoute); got != "192.168.1.1" {
		t.Fatalf("ParseWindowsRoutePrint = %q, want 192.168.1.1", got)
	}

	winIPConfig := `
Ethernet adapter Ethernet:
   IPv4 Address. . . . . . . . . . . : 192.168.8.101
   Default Gateway . . . . . . . . . :
Wireless LAN adapter Wi-Fi:
   Default Gateway . . . . . . . . . : 192.168.100.1`
	if got := ParseWindowsIPConfig(winIPConfig); got != "192.168.100.1" {
		t.Fatalf("ParseWindowsIPConfig = %q, want 192.168.100.1", got)
	}

	procRoute := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"eth0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"wlan0\t00000000\tFE01A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n"
	if got := ParseProcNetRoute(procRoute); got != "192.168.1.1" {
		t.Fatalf("ParseProcNetRoute = %q, want 192.168.1.1", got)
	}

	if got := ParseLinuxIPRoute("default via 10.20.30.1 dev wlan0 proto dhcp metric 600"); got != "10.20.30.1" {
		t.Fatalf("ParseLinuxIPRoute = %q, want 10.20.30.1", got)
	}

	macRoute := "   route to: default\n destination: default\n     gateway: 172.16.0.1\n"
	if got := ParseMacRouteGet(macRoute); got != "172.16.0.1" {
		t.Fatalf("ParseMacRouteGet = %q, want 172.16.0.1", got)
	}
}

func TestSessionCookieJarAndSanitization(t *testing.T) {
	jar := NewSessionCookieJar(true)
	if hdr := jar.GetMergedCookieHeader(""); !strings.Contains(hdr, "FirstMenu=Admin_0") {
		t.Fatalf("expected Huawei seed cookie, got %q", hdr)
	}

	jar.SaveCookiesFromHeaders([]string{
		"SessionID=tok123; Path=/; Secure; HttpOnly",
		"Language=ar; Path=/",
	})

	merged := jar.GetMergedCookieHeader("BrowserExtra=abc; FirstMenu=Custom")
	if !strings.Contains(merged, "SessionID=tok123") ||
		!strings.Contains(merged, "Language=ar") ||
		!strings.Contains(merged, "BrowserExtra=abc") ||
		!strings.Contains(merged, "FirstMenu=Custom") {
		t.Fatalf("unexpected merged cookie header: %q", merged)
	}

	// Expire and delete cookies
	jar.SaveCookiesFromHeaders([]string{
		"SessionID=deleted; Path=/",
		"Language=ar; Max-Age=0; Path=/",
	})
	snap := jar.Snapshot()
	if _, ok := snap["SessionID"]; ok {
		t.Fatalf("SessionID should have been removed")
	}
	if _, ok := snap["Language"]; ok {
		t.Fatalf("Language should have been removed on Max-Age=0")
	}

	evicted := jar.Clear()
	if len(evicted) == 0 {
		t.Fatalf("expected evicted cookie keys on Clear")
	}
	if got := jar.Snapshot()["FirstMenu"]; got != "Admin_0" {
		t.Fatalf("expected Huawei cookie re-seeded after Clear, got %q", got)
	}

	cleaned := CleanSetCookieHeader("SID=xyz; Domain=192.168.1.1; Secure; SameSite=None")
	if strings.Contains(strings.ToLower(cleaned), "domain=") ||
		strings.Contains(strings.ToLower(cleaned), "; secure") ||
		!strings.Contains(cleaned, "SameSite=Lax") ||
		!strings.Contains(cleaned, "Path=/") {
		t.Fatalf("unexpected CleanSetCookieHeader output: %q", cleaned)
	}
}

func TestTransformerRewritingAndBinarySafety(t *testing.T) {
	input := `
		<script>
			top.location.href = 'https://192.168.1.1:443/index.asp';
			var api = "https:\/\/192.168.1.1\/api";
			var proto = '//192.168.1.1/app.js';
			var hostA = '192.168.1.1';
			var hostB = "192.168.1.1:8443";
		</script>
	`
	out := RewriteResponseBody(input, "192.168.1.1:8443", 8080, "localhost")
	for _, unexpected := range []string{"192.168.1.1"} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("expected all %q occurrences rewritten, got:\n%s", unexpected, out)
		}
	}
	for _, expected := range []string{
		"http://localhost:8080/index.asp",
		`http:\/\/localhost:8080\/api`,
		"//localhost:8080/app.js",
		"'localhost:8080'",
		`"localhost:8080"`,
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("expected %q in rewritten body:\n%s", expected, out)
		}
	}

	// Redirect rewriting (router IP, single-label BMC host, and external URL preserved)
	if got := RewriteRedirectLocation("https://192.168.1.1:443/html/main.html", "192.168.1.1", 8080, "127.0.0.1", true); got != "http://127.0.0.1:8080/html/main.html" {
		t.Fatalf("RewriteRedirectLocation target IP = %q", got)
	}
	if got := RewriteRedirectLocation("https://idrac-r610/login.html", "192.168.0.120", 8080, "localhost", true); got != "http://localhost:8080/login.html" {
		t.Fatalf("RewriteRedirectLocation single-label BMC = %q", got)
	}
	if got := RewriteRedirectLocation("https://support.dell.com/docs", "192.168.0.120", 8080, "localhost", true); got != "https://support.dell.com/docs" {
		t.Fatalf("expected external redirect untouched, got %q", got)
	}

	// Binary safety when Content-Type header is omitted
	gifBytes := []byte{'G', 'I', 'F', '8', '9', 'a', 0x01, 0x00, 0xFF, 0xFE}
	emptyHdr := make(http.Header)
	mime := GetAccurateMimeType("/images/logo.gif", emptyHdr, gifBytes)
	if mime != "image/gif" {
		t.Fatalf("GetAccurateMimeType(.gif) = %q, want image/gif", mime)
	}
	if IsTransformableText("/images/logo.gif", mime, gifBytes) {
		t.Fatalf("GIF image must never be flagged as transformable text")
	}

	unknownBinary := []byte{0x00, 0x01, 0x02, 0xFF, '1', '9', '2', '.', '1', '6', '8'}
	if IsTransformableText("/download/fw", "text/html; charset=utf-8", unknownBinary) {
		t.Fatalf("payload containing null bytes must not be transformed as text")
	}

	if resMime := GetAccurateMimeType("/lang/footer.res", emptyHdr, []byte("var x=1;")); resMime != "application/javascript; charset=utf-8" {
		t.Fatalf("expected .res MIME application/javascript, got %q", resMime)
	}
}

func TestLenientRawHTTPParser(t *testing.T) {
	t.Run("HTTP/1.0 without status text or Content-Length", func(t *testing.T) {
		raw := "HTTP/1.0 200\r\nSet-Cookie: A=1\r\nSet-Cookie: B=2\r\n\r\n<html>ok</html>"
		resp, err := parseRawHTTPResponse(bufio.NewReader(strings.NewReader(raw)), http.MethodGet)
		if err != nil {
			t.Fatalf("parseRawHTTPResponse failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
		}
		if len(resp.Headers.Values("Set-Cookie")) != 2 {
			t.Fatalf("expected 2 Set-Cookie headers, got %v", resp.Headers.Values("Set-Cookie"))
		}
		if string(resp.Body) != "<html>ok</html>" {
			t.Fatalf("Body = %q", string(resp.Body))
		}
	})

	t.Run("chunked transfer encoding with extensions", func(t *testing.T) {
		raw := "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n5;ext=1\r\nhello\r\n6\r\n world\r\n0\r\n\r\n"
		resp, err := parseRawHTTPResponse(bufio.NewReader(strings.NewReader(raw)), http.MethodGet)
		if err != nil {
			t.Fatalf("parseRawHTTPResponse failed: %v", err)
		}
		if string(resp.Body) != "hello world" {
			t.Fatalf("chunked Body = %q, want %q", string(resp.Body), "hello world")
		}
	})

	t.Run("gzip encoded response body", func(t *testing.T) {
		var gz bytes.Buffer
		w := gzip.NewWriter(&gz)
		_, _ = w.Write([]byte("compressed-router-html"))
		_ = w.Close()

		raw := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Encoding: gzip\r\nContent-Length: %d\r\n\r\n", gz.Len())
		full := append([]byte(raw), gz.Bytes()...)
		resp, err := parseRawHTTPResponse(bufio.NewReader(bytes.NewReader(full)), http.MethodGet)
		if err != nil {
			t.Fatalf("parseRawHTTPResponse failed: %v", err)
		}
		if string(resp.Body) != "compressed-router-html" {
			t.Fatalf("gzip Body = %q", string(resp.Body))
		}
	})
}

func TestEndToEndLegacyTLS10AndStaticCaching(t *testing.T) {
	cert := generateSelfSignedRSA2048Cert(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	routerAddr := ln.Addr().String()
	_, routerPortStr, _ := net.SplitHostPort(routerAddr)
	gifPayload := []byte{'G', 'I', 'F', '8', '9', 'a', 0x01, 0x00, 0xFF, 0x00, 0xFE, 0x3B}
	var gifHits atomic.Int32
	var seenSessionCookie atomic.Value

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS10,
		MaxVersion:   tls.VersionTLS10,
		CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA},
	}
	tlsListener := tls.NewListener(ln, tlsCfg)
	defer tlsListener.Close()

	go func() {
		for {
			conn, err := tlsListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				req, err := http.ReadRequest(bufio.NewReader(c))
				if err != nil {
					return
				}
				switch req.URL.Path {
				case "/login":
					_, _ = fmt.Fprintf(c,
						"HTTP/1.1 302 Found\r\n"+
							"Set-Cookie: SessionID=router-auth-99; Domain=127.0.0.1; Secure; SameSite=None\r\n"+
							"Location: https://127.0.0.1:%s/html/main.html\r\n"+
							"Content-Length: 0\r\n\r\n",
						routerPortStr,
					)
				case "/html/main.html":
					seenSessionCookie.Store(req.Header.Get("Cookie"))
					body := fmt.Sprintf(
						`<script>top.location.href='https://127.0.0.1:%s/home.asp'; var a="127.0.0.1:%s";</script>`,
						routerPortStr, routerPortStr,
					)
					_, _ = fmt.Fprintf(c,
						"HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: %d\r\n\r\n%s",
						len(body), body,
					)
				case "/images/logo.gif":
					gifHits.Add(1)
					// Omit Content-Type intentionally to verify binary protection.
					_, _ = fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", len(gifPayload))
					_, _ = c.Write(gifPayload)
				default:
					_, _ = io.WriteString(c, "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n")
				}
			}(conn)
		}
	}()

	srv := NewServer(Options{
		InitialTarget:   "https://" + routerAddr,
		InitialPresetID: "huawei-zte",
		DetectedGateway: "192.168.1.1",
	})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	var proxyPort int
	_, portStr, _ := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	_, _ = fmt.Sscanf(portStr, "%d", &proxyPort)
	srv.SetProxyPort(proxyPort)

	httpClient := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. Login redirect + Set-Cookie sanitization
	resp, err := httpClient.Get(ts.URL + "/login")
	if err != nil {
		t.Fatalf("GET /login failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("GET /login status = %d, want 302", resp.StatusCode)
	}
	wantLoc := fmt.Sprintf("http://127.0.0.1:%d/html/main.html", proxyPort)
	if gotLoc := resp.Header.Get("Location"); gotLoc != wantLoc {
		t.Fatalf("Location = %q, want %q", gotLoc, wantLoc)
	}
	sc := resp.Header.Get("Set-Cookie")
	if strings.Contains(strings.ToLower(sc), "secure") || strings.Contains(strings.ToLower(sc), "domain=") {
		t.Fatalf("Set-Cookie was not sanitized: %q", sc)
	}

	// 2. HTML body rewriting + server-side cookie jar injection (even without browser Cookie header)
	resp2, err := httpClient.Get(ts.URL + "/html/main.html")
	if err != nil {
		t.Fatalf("GET /html/main.html failed: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if !strings.Contains(string(body2), fmt.Sprintf("http://127.0.0.1:%d/home.asp", proxyPort)) {
		t.Fatalf("HTML URL was not rewritten:\n%s", string(body2))
	}
	if strings.Contains(string(body2), routerPortStr) {
		t.Fatalf("upstream router port leaked in rewritten HTML:\n%s", string(body2))
	}
	cookieSent, _ := seenSessionCookie.Load().(string)
	if !strings.Contains(cookieSent, "SessionID=router-auth-99") || !strings.Contains(cookieSent, "FirstMenu=Admin_0") {
		t.Fatalf("expected server jar cookies forwarded upstream, got %q", cookieSent)
	}

	// 3. Binary GIF safety + Static Asset Cache hit on second request
	for i := 0; i < 2; i++ {
		rGif, gErr := httpClient.Get(ts.URL + "/images/logo.gif")
		if gErr != nil {
			t.Fatalf("GET /images/logo.gif attempt %d failed: %v", i, gErr)
		}
		gotBytes, _ := io.ReadAll(rGif.Body)
		_ = rGif.Body.Close()
		if !bytes.Equal(gotBytes, gifPayload) {
			t.Fatalf("GIF bytes corrupted! got %v, want %v", gotBytes, gifPayload)
		}
		if ct := rGif.Header.Get("Content-Type"); ct != "image/gif" {
			t.Fatalf("GIF Content-Type = %q, want image/gif", ct)
		}
	}
	if hits := gifHits.Load(); hits != 1 {
		t.Fatalf("expected 1 upstream GIF hit due to static cache, got %d", hits)
	}

	// 4. Live TLS Telemetry verification
	snap := srv.Snapshot()
	if snap.TLS.Protocol != "TLS 1.0" {
		t.Fatalf("expected TLS 1.0 telemetry, got %+v", snap.TLS)
	}
	if !strings.Contains(snap.TLS.CipherSuite, "TLS_RSA_WITH_AES_128_CBC_SHA") {
		t.Fatalf("expected TLS_RSA_WITH_AES_128_CBC_SHA cipher telemetry, got %+v", snap.TLS)
	}
}

func TestPlainHTTPFallbackOnCustomPort(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "plain-http-router-ok")
	}))
	defer upstream.Close()

	// Strip http:// so the proxy initially assumes TLS on this custom port and falls back on RecordHeaderError.
	hostPort := strings.TrimPrefix(upstream.URL, "http://")

	srv := NewServer(Options{
		InitialTarget:   hostPort,
		InitialPresetID: "auto",
	})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("proxy GET failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "plain-http-router-ok" {
		t.Fatalf("unexpected fallback response: %d %q", resp.StatusCode, string(body))
	}
	if snap := srv.Snapshot(); snap.TLS.Transport != "Plain HTTP" {
		t.Fatalf("expected Plain HTTP telemetry after fallback, got %+v", snap.TLS)
	}
}

func TestAbruptTLSCloseWithoutCloseNotify(t *testing.T) {
	cert := generateSelfSignedRSA2048Cert(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			rawConn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(rc net.Conn) {
				defer rc.Close()
				tc := tls.Server(rc, &tls.Config{
					Certificates: []tls.Certificate{cert},
					MinVersion:   tls.VersionTLS10,
					MaxVersion:   tls.VersionTLS12,
				})
				if err := tc.Handshake(); err != nil {
					return
				}
				_, _ = http.ReadRequest(bufio.NewReader(tc))
				// Send HTTP/1.0 response without Content-Length and close rawConn WITHOUT tls.Conn.Close() (no close_notify).
				_, _ = io.WriteString(tc, "HTTP/1.0 200 OK\r\nContent-Type: text/plain\r\n\r\nabrupt-eof-payload")
				_ = rc.Close()
			}(rawConn)
		}
	}()

	client := NewClient(4, nil)
	resp, _, err := client.SendRequest(
		context.Background(),
		"https://"+ln.Addr().String(),
		FindPreset("auto"),
		UpstreamRequest{Method: http.MethodGet, Path: "/"},
		"",
	)
	if err != nil {
		t.Fatalf("expected abrupt EOF without close_notify to succeed, got error: %v", err)
	}
	if string(resp.Body) != "abrupt-eof-payload" {
		t.Fatalf("Body = %q, want %q", string(resp.Body), "abrupt-eof-payload")
	}
}

func TestBridgeControlAPIAndSessionReset(t *testing.T) {
	srv := NewServer(Options{
		ProxyPort:       8080,
		InitialTarget:   "192.168.1.1",
		InitialPresetID: "auto",
		DetectedGateway: "192.168.1.1",
	})
	srv.activeJar().SaveCookiesFromHeaders([]string{"SessionID=stuck-session; Path=/"})

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// 1. Launchpad HTML renders
	res, err := http.Get(ts.URL + "/_bridge")
	if err != nil {
		t.Fatalf("GET /_bridge failed: %v", err)
	}
	htmlBytes, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	bodyStr := string(htmlBytes)
	if res.StatusCode != http.StatusOK ||
		!strings.Contains(bodyStr, `name="color-scheme" content="light dark"`) ||
		!strings.Contains(bodyStr, `Open Router Page`) ||
		!strings.Contains(bodyStr, `<details id="options-panel">`) ||
		!strings.Contains(bodyStr, `<details id="logs-panel">`) {
		t.Fatalf("unexpected /_bridge response: status=%d", res.StatusCode)
	}

	// 2. Ping endpoint sets proxy header
	pingRes, err := http.Get(ts.URL + "/_bridge/api/ping")
	if err != nil {
		t.Fatalf("GET /_bridge/api/ping failed: %v", err)
	}
	_ = pingRes.Body.Close()
	if pingRes.Header.Get("X-Legacy-Router-Proxy") != "1" {
		t.Fatalf("missing X-Legacy-Router-Proxy header on ping")
	}

	// 3. Invalid config rejected
	badRes, err := http.Post(ts.URL+"/_bridge/api/config", "application/json", strings.NewReader(`{"target":"bad host"}`))
	if err != nil {
		t.Fatalf("POST /_bridge/api/config failed: %v", err)
	}
	_ = badRes.Body.Close()
	if badRes.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for whitespace target, got %d", badRes.StatusCode)
	}

	// 4. Valid config switches target and preset (HPE iLO caps sockets to 2, disables Huawei cookies)
	cfgRes, err := http.Post(
		ts.URL+"/_bridge/api/config",
		"application/json",
		strings.NewReader(`{"target":"10.10.10.50:443","preset":"ilo"}`),
	)
	if err != nil {
		t.Fatalf("POST /_bridge/api/config failed: %v", err)
	}
	var snap StateSnapshot
	_ = json.NewDecoder(cfgRes.Body).Decode(&snap)
	_ = cfgRes.Body.Close()
	if snap.Target != "10.10.10.50:443" || snap.Preset != "ilo" || snap.MaxSockets != 2 {
		t.Fatalf("unexpected snapshot after config update: %+v", snap)
	}

	// Verify /_bridge SSR-hydrates the updated target into the HTML immediately
	reloadedRes, err := http.Get(ts.URL + "/_bridge")
	if err != nil {
		t.Fatalf("GET /_bridge after config failed: %v", err)
	}
	reloadedBytes, _ := io.ReadAll(reloadedRes.Body)
	_ = reloadedRes.Body.Close()
	if !strings.Contains(string(reloadedBytes), `Open Router Page (10.10.10.50:443)`) {
		t.Fatalf("expected updated target hydrated in /_bridge HTML")
	}

	// 5. Session reset clears cookies & emits browser eviction headers
	srv.activeJar().SaveCookiesFromHeaders([]string{"iLOSession=abc; Path=/"})
	resetRes, err := http.Post(ts.URL+"/_bridge/api/reset", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /_bridge/api/reset failed: %v", err)
	}
	_ = resetRes.Body.Close()
	if resetRes.Header.Get("Clear-Site-Data") != `"cookies"` {
		t.Fatalf("expected Clear-Site-Data header, got %q", resetRes.Header.Get("Clear-Site-Data"))
	}
	if setCookies := strings.Join(resetRes.Header.Values("Set-Cookie"), "\n"); !strings.Contains(setCookies, "iLOSession=; Path=/; Max-Age=0") {
		t.Fatalf("expected Max-Age=0 eviction header for iLOSession, got %q", setCookies)
	}
}

func generateSelfSignedRSA2048Cert(t *testing.T) tls.Certificate {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("x509.CreateCertificate: %v", err)
	}
	return tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}
}
