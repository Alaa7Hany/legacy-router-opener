package proxy

import (
	"bufio"
	"bytes"
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

	"github.com/alaa/legacyrouteropener/internal/upstream"
)

// boundControlPlane binds both loopback origins before constructing the Server,
// so every advertised port matches a real listener and no post-construction
// setter is needed.
type boundControlPlane struct {
	server      *Server
	proxy       *httptest.Server
	control     *httptest.Server
	proxyPort   int
	controlPort int
}

func newBoundControlPlane(t *testing.T, opts Options) *boundControlPlane {
	t.Helper()
	proxyLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	controlLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		_ = proxyLn.Close()
		t.Fatal(err)
	}
	opts.ProxyPort = proxyLn.Addr().(*net.TCPAddr).Port
	opts.ControlPort = controlLn.Addr().(*net.TCPAddr).Port
	if opts.RequestedPort == 0 {
		opts.RequestedPort = opts.ProxyPort
	}
	srv := NewServer(opts)

	proxyTS := httptest.NewUnstartedServer(srv.ProxyHandler())
	_ = proxyTS.Listener.Close()
	proxyTS.Listener = proxyLn
	proxyTS.Start()
	controlTS := httptest.NewUnstartedServer(srv.ControlHandler())
	_ = controlTS.Listener.Close()
	controlTS.Listener = controlLn
	controlTS.Start()
	t.Cleanup(func() {
		proxyTS.Close()
		controlTS.Close()
	})
	return &boundControlPlane{
		server: srv, proxy: proxyTS, control: controlTS,
		proxyPort: opts.ProxyPort, controlPort: opts.ControlPort,
	}
}

// readStateSnapshot reads the control-plane state through the HTTP seam.
func readStateSnapshot(t *testing.T, controlURL string) StateSnapshot {
	t.Helper()
	res, err := http.Get(controlURL + "/_bridge/api/state")
	if err != nil {
		t.Fatalf("GET /_bridge/api/state: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /_bridge/api/state status = %d, want 200", res.StatusCode)
	}
	var snap StateSnapshot
	if err := json.NewDecoder(res.Body).Decode(&snap); err != nil {
		t.Fatalf("decode state snapshot: %v", err)
	}
	return snap
}

// primeProxyTraffic drives real traffic through the proxy so the device jar,
// static cache and transport telemetry hold observable state.
func primeProxyTraffic(t *testing.T, proxyURL string, paths ...string) {
	t.Helper()
	for _, path := range paths {
		res, err := http.Get(proxyURL + path)
		if err != nil {
			t.Fatalf("GET %s through proxy: %v", path, err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s through proxy status = %d, want 200", path, res.StatusCode)
		}
	}
}

// postControlJSON posts a control-origin request with the JSON content type and
// CSRF headers the control handler requires.
func postControlJSON(t *testing.T, controlURL, path string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, controlURL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", controlURL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return res
}

// postConfigRequest switches target/preset through the control-origin config API.
func postConfigRequest(t *testing.T, controlURL, target, preset string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]string{"target": target, "preset": preset})
	if err != nil {
		t.Fatal(err)
	}
	return postControlJSON(t, controlURL, "/_bridge/api/config", body)
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

	plane := newBoundControlPlane(t, Options{
		InitialTarget:   "https://" + routerAddr,
		InitialPresetID: "huawei-zte",
		DetectedGateway: "192.168.1.1",
	})
	srv := plane.server
	ts := plane.proxy
	proxyPort := plane.proxyPort

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

	plane := newBoundControlPlane(t, Options{
		InitialTarget:   hostPort,
		InitialPresetID: "auto",
	})
	srv := plane.server
	ts := plane.proxy

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

	client := upstream.NewClient(4, nil)
	resp, _, err := client.SendRequest(
		context.Background(),
		"https://"+ln.Addr().String(),
		upstream.FindPreset("auto"),
		upstream.UpstreamRequest{Method: http.MethodGet, Path: "/"},
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
	plane := newBoundControlPlane(t, Options{
		InitialTarget:   "192.168.1.1",
		InitialPresetID: "auto",
		DetectedGateway: "192.168.1.1",
	})
	srv := plane.server
	srv.profile.activeJar().SaveCookiesFromHeaders([]string{"SessionID=stuck-session; Path=/"})
	ts := plane.control

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
		!strings.Contains(bodyStr, `>Open router page</span>`) ||
		!strings.Contains(bodyStr, `<summary id="language-switch"`) ||
		!strings.Contains(bodyStr, `data-language="en" aria-pressed="true"`) ||
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
	badRes := postControlJSON(t, ts.URL, "/_bridge/api/config", []byte(`{"target":"bad host"}`))
	_ = badRes.Body.Close()
	if badRes.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for whitespace target, got %d", badRes.StatusCode)
	}

	// 4. Valid config switches target and preset (HPE iLO caps sockets to 2, disables Huawei cookies)
	cfgRes := postControlJSON(t, ts.URL, "/_bridge/api/config", []byte(`{"target":"10.10.10.50:443","preset":"ilo"}`))
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
	if !strings.Contains(string(reloadedBytes), `id="target-display" dir="ltr">10.10.10.50:443</bdi>`) {
		t.Fatalf("expected updated target hydrated in /_bridge HTML")
	}

	// Also verify explicit ?lang=ar query parameter overrides Accept-Language on /_bridge.
	arRes, err := http.Get(ts.URL + "/_bridge?lang=ar")
	if err != nil {
		t.Fatalf("GET /_bridge?lang=ar failed: %v", err)
	}
	arBytes, _ := io.ReadAll(arRes.Body)
	_ = arRes.Body.Close()
	if !strings.Contains(string(arBytes), `<html lang="ar" dir="rtl">`) {
		t.Fatalf("expected ?lang=ar to render Arabic RTL HTML")
	}

	// 5. Session reset clears cookies & emits browser eviction headers
	srv.profile.activeJar().SaveCookiesFromHeaders([]string{"iLOSession=abc; Path=/"})
	stateBefore, err := http.Get(ts.URL + "/_bridge/api/state")
	if err != nil {
		t.Fatalf("GET /_bridge/api/state failed: %v", err)
	}
	var beforeReset StateSnapshot
	_ = json.NewDecoder(stateBefore.Body).Decode(&beforeReset)
	_ = stateBefore.Body.Close()
	if beforeReset.CookieCount == 0 {
		t.Fatalf("expected a saved cookie before reset, got %+v", beforeReset)
	}

	resetRes := postControlJSON(t, ts.URL, "/_bridge/api/reset", nil)
	_ = resetRes.Body.Close()
	if resetRes.Header.Get("Clear-Site-Data") != `"cookies"` {
		t.Fatalf("expected Clear-Site-Data header, got %q", resetRes.Header.Get("Clear-Site-Data"))
	}
	if setCookies := strings.Join(resetRes.Header.Values("Set-Cookie"), "\n"); !strings.Contains(setCookies, "iLOSession=; Path=/; Max-Age=0") {
		t.Fatalf("expected Max-Age=0 eviction header for iLOSession, got %q", setCookies)
	}

	stateAfter, err := http.Get(ts.URL + "/_bridge/api/state")
	if err != nil {
		t.Fatalf("GET /_bridge/api/state after reset failed: %v", err)
	}
	var afterReset StateSnapshot
	_ = json.NewDecoder(stateAfter.Body).Decode(&afterReset)
	_ = stateAfter.Body.Close()
	if afterReset.CookieCount != 0 {
		t.Fatalf("cookie count after reset = %d, want 0", afterReset.CookieCount)
	}
}

func TestResetClearsDeviceProfileStateThroughControlAPI(t *testing.T) {
	// A Device profile that does not seed Huawei cookies keeps the jar initially
	// empty, so the state readout can show the cookie count fall all the way to zero.
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set("Set-Cookie", "SessionID=stuck-session; Path=/")
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>login</html>")
		case "/app.js":
			w.Header().Set("Content-Type", "application/javascript")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			_, _ = io.WriteString(w, "var app=1;")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstreamSrv.Close()

	plane := newBoundControlPlane(t, Options{
		InitialTarget:   upstreamSrv.URL,
		InitialPresetID: "ilo",
	})
	proxyTS := plane.proxy
	controlTS := plane.control

	// Drive real upstream traffic so the jar, static cache and telemetry all hold stale state.
	primeProxyTraffic(t, proxyTS.URL, "/login", "/app.js")

	before := readStateSnapshot(t, controlTS.URL)
	if before.CookieCount < 1 {
		t.Fatalf("expected a saved session cookie before reset, got %+v", before)
	}
	if before.CachedAssets < 1 {
		t.Fatalf("expected a cached asset before reset, got %+v", before)
	}
	if before.TLS.Transport != "Plain HTTP" {
		t.Fatalf("expected live upstream transport telemetry before reset, got %+v", before.TLS)
	}

	resetRes := postControlJSON(t, controlTS.URL, "/_bridge/api/reset", nil)
	_ = resetRes.Body.Close()
	if resetRes.StatusCode != http.StatusOK {
		t.Fatalf("POST reset status = %d, want 200", resetRes.StatusCode)
	}
	if got := resetRes.Header.Get("Clear-Site-Data"); got != `"cookies"` {
		t.Fatalf("Clear-Site-Data = %q, want %q", got, `"cookies"`)
	}
	if got := strings.Join(resetRes.Header.Values("Set-Cookie"), "\n"); !strings.Contains(got, "SessionID=; Path=/; Max-Age=0") {
		t.Fatalf("missing session cookie eviction header, got %q", got)
	}

	after := readStateSnapshot(t, controlTS.URL)
	if after.CookieCount != 0 {
		t.Fatalf("cookie count after reset = %d, want 0", after.CookieCount)
	}
	if after.CachedAssets != 0 {
		t.Fatalf("cached asset count after reset = %d, want 0", after.CachedAssets)
	}
	if after.TLS.Transport != "Idle" {
		t.Fatalf("transport after reset = %q, want Idle", after.TLS.Transport)
	}
}

func TestProfileSwitchThroughControlAPI(t *testing.T) {
	// Two simulated devices let the switch be observed as a move from one
	// endpoint to the other through the Opener's control origin only.
	deviceA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set("Set-Cookie", "DeviceSession=abc; Path=/")
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>login</html>")
		case "/app.js":
			w.Header().Set("Content-Type", "application/javascript")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			_, _ = io.WriteString(w, "var device=1;")
		default:
			http.NotFound(w, r)
		}
	}))
	defer deviceA.Close()
	deviceB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>device-b</html>")
	}))
	defer deviceB.Close()

	// Bind both local origins before construction so the control port checked
	// by CSRF protection is the port that actually serves the Opener.
	proxyLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind proxy listener: %v", err)
	}
	controlLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind control listener: %v", err)
	}
	srv := NewServer(Options{
		ProxyPort:       proxyLn.Addr().(*net.TCPAddr).Port,
		ControlPort:     controlLn.Addr().(*net.TCPAddr).Port,
		InitialTarget:   deviceA.URL,
		InitialPresetID: "ilo",
	})
	startOn := func(ln net.Listener, handler http.Handler) *httptest.Server {
		t.Helper()
		ts := httptest.NewUnstartedServer(handler)
		_ = ts.Listener.Close()
		ts.Listener = ln
		ts.Start()
		t.Cleanup(ts.Close)
		return ts
	}
	proxyTS := startOn(proxyLn, srv.ProxyHandler())
	controlTS := startOn(controlLn, srv.ControlHandler())

	// Populate the cookie jar and static cache for the first device.
	primeProxyTraffic(t, proxyTS.URL, "/login", "/app.js")

	initial := readStateSnapshot(t, controlTS.URL)
	if initial.Target != deviceA.URL || initial.Preset != "ilo" || initial.MaxSockets != 2 {
		t.Fatalf("unexpected initial device state: %+v", initial)
	}
	if initial.CachedAssets < 1 || initial.CookieCount < 1 {
		t.Fatalf("expected a cached asset and saved cookie before switching: %+v", initial)
	}

	// Switching the Device profile on the same endpoint changes the socket cap,
	// invalidates the cache and reseeds that device's cookies.
	res := postConfigRequest(t, controlTS.URL, deviceA.URL, "auto")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("profile switch status = %d, want 200", res.StatusCode)
	}
	_ = res.Body.Close()
	profileSwitched := readStateSnapshot(t, controlTS.URL)
	if profileSwitched.Target != deviceA.URL || profileSwitched.Preset != "auto" || profileSwitched.MaxSockets != 4 {
		t.Fatalf("unexpected state after profile switch: %+v", profileSwitched)
	}
	if profileSwitched.CachedAssets != 0 {
		t.Fatalf("cached assets not invalidated on profile change: %+v", profileSwitched)
	}
	if profileSwitched.CookieCount < 4 {
		t.Fatalf("device-specific cookies not reseeded on profile change: %+v", profileSwitched)
	}

	// Re-populate the cache, then move to a different device endpoint.
	primeProxyTraffic(t, proxyTS.URL, "/app.js")
	if readStateSnapshot(t, controlTS.URL).CachedAssets < 1 {
		t.Fatalf("expected a cached asset before endpoint switch")
	}
	res = postConfigRequest(t, controlTS.URL, deviceB.URL, "auto")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("endpoint switch status = %d, want 200", res.StatusCode)
	}
	_ = res.Body.Close()
	endpointSwitched := readStateSnapshot(t, controlTS.URL)
	if endpointSwitched.Target != deviceB.URL || endpointSwitched.Preset != "auto" {
		t.Fatalf("unexpected state after endpoint switch: %+v", endpointSwitched)
	}
	if endpointSwitched.CachedAssets != 0 {
		t.Fatalf("cached assets not invalidated on endpoint change: %+v", endpointSwitched)
	}
	targetLogged := false
	for _, line := range endpointSwitched.Logs {
		if strings.Contains(line, "[TARGET]") && strings.Contains(line, deviceB.URL) {
			targetLogged = true
			break
		}
	}
	if !targetLogged {
		t.Fatalf("target log line missing for %s: %v", deviceB.URL, endpointSwitched.Logs)
	}

	// Validation and self-target checks still cross the same switch seam.
	for _, tc := range []struct {
		name   string
		target string
	}{
		{name: "invalid", target: "bad host"},
		{name: "self", target: fmt.Sprintf("http://127.0.0.1:%d", controlLn.Addr().(*net.TCPAddr).Port)},
	} {
		res := postConfigRequest(t, controlTS.URL, tc.target, "")
		_ = res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s target status = %d, want 400", tc.name, res.StatusCode)
		}
	}
	if still := readStateSnapshot(t, controlTS.URL); still.Target != deviceB.URL {
		t.Fatalf("rejected switch changed the active target: %+v", still)
	}
}

func TestDesktopPagesUseBrowserLanguage(t *testing.T) {
	// Use a port that refuses immediately, so the request reaches the app's own error page.
	conn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target := "http://" + conn.Addr().String()
	conn.Close()
	srv := NewServer(Options{
		ProxyPort:     8080,
		ControlPort:   8180,
		InitialTarget: target,
	})
	for _, tc := range []struct {
		handler http.Handler
		url     string
		label   string
		status  int
	}{
		{srv.ControlHandler(), "http://localhost:8180/_bridge", "فتح صفحة الراوتر", http.StatusOK},
		{srv.ProxyHandler(), "http://127.0.0.1:8080/", "تعذّر فتح صفحة الجهاز", http.StatusBadGateway},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.url, nil)
		req.Header.Set("Accept-Language", "ar-EG,en;q=0.8")
		res := httptest.NewRecorder()
		tc.handler.ServeHTTP(res, req)
		body := res.Body.String()
		if res.Code != tc.status || !strings.Contains(body, `<html lang="ar" dir="rtl">`) ||
			!strings.Contains(body, `data-language="ar" aria-pressed="true"`) ||
			!strings.Contains(body, tc.label) || !strings.Contains(body, `dir="ltr">`+target+`</bdi>`) {
			t.Errorf("%s did not render the expected Arabic page and left-to-right address (status %d)", tc.url, res.Code)
		}
		if tc.status == http.StatusBadGateway {
			if !strings.Contains(body, `href="http://127.0.0.1:8180/_bridge?edit=1&amp;lang=ar"`) {
				t.Errorf("error page did not point to control opener with matching loopback host and lang")
			}
			if res.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("error page missing Cache-Control: no-store")
			}
		}
	}
}

func TestOriginSeparationCSRFAndDNSRebinding(t *testing.T) {
	srv := NewServer(Options{
		ProxyPort:     8080,
		ControlPort:   8180,
		RequestedPort: 8080,
		InitialTarget: "192.168.1.1",
	})
	proxyH := srv.ProxyHandler()
	controlH := srv.ControlHandler()

	// 1. Proxy server does NOT serve control APIs, assets, SSE, or /quit, and redirects /_bridge to control origin.
	redirReq := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/_bridge?lang=ar", nil)
	redirRes := httptest.NewRecorder()
	proxyH.ServeHTTP(redirRes, redirReq)
	if redirRes.Code != http.StatusFound || redirRes.Header().Get("Location") != "http://127.0.0.1:8180/_bridge?lang=ar" {
		t.Fatalf("GET /_bridge on proxy = (%d, %q), want 302 -> control origin", redirRes.Code, redirRes.Header().Get("Location"))
	}

	pingReq := httptest.NewRequest(http.MethodGet, "http://localhost:8080/_bridge/api/ping", nil)
	pingRes := httptest.NewRecorder()
	proxyH.ServeHTTP(pingRes, pingReq)
	if pingRes.Code != http.StatusOK ||
		pingRes.Header().Get("X-Legacy-Router-Proxy") != "1" ||
		pingRes.Header().Get("X-Legacy-Router-Requested-Port") != "8080" ||
		pingRes.Header().Get("X-Legacy-Router-Control-Port") != "8180" {
		t.Fatalf("ping headers missing on proxy: status=%d headers=%v", pingRes.Code, pingRes.Header())
	}

	for _, blockedOnProxy := range []struct{ method, path string }{
		{http.MethodGet, "/_bridge/api/state"},
		{http.MethodPost, "/_bridge/api/config"},
		{http.MethodPost, "/_bridge/api/reset"},
		{http.MethodGet, "/_bridge/events"},
		{http.MethodGet, "/_bridge/assets/bridge.js"},
	} {
		req := httptest.NewRequest(blockedOnProxy.method, "http://localhost:8080"+blockedOnProxy.path, nil)
		res := httptest.NewRecorder()
		proxyH.ServeHTTP(res, req)
		if res.Code != http.StatusNotFound {
			t.Errorf("proxy %s %s = %d, want 404", blockedOnProxy.method, blockedOnProxy.path, res.Code)
		}
	}

	// 2. DNS rebinding Host rejection on both Proxy and Control servers.
	for _, tc := range []struct {
		name    string
		handler http.Handler
		host    string
		path    string
		want    int
	}{
		{"evil host on proxy", proxyH, "attacker.evil.example:8080", "/_bridge/api/ping", http.StatusNotFound},
		{"control host on proxy", proxyH, "localhost:8180", "/_bridge/api/ping", http.StatusNotFound},
		{"evil host on control", controlH, "attacker.evil.example:8180", "/_bridge/api/state", http.StatusNotFound},
		{"proxy host on control", controlH, "localhost:8080", "/_bridge/api/state", http.StatusNotFound},
		{"uppercase localhost allowed on control", controlH, "LOCALHOST:8180", "/_bridge/api/ping", http.StatusOK},
		{"IPv6 loopback allowed on control", controlH, "[::1]:8180", "/_bridge/api/ping", http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://"+tc.host+tc.path, nil)
		req.Host = tc.host
		res := httptest.NewRecorder()
		tc.handler.ServeHTTP(res, req)
		if res.Code != tc.want {
			t.Errorf("%s: got status %d, want %d", tc.name, res.Code, tc.want)
		}
	}

	// 3. CSRF rejection on all state-changing control endpoints.
	for _, endpoint := range []string{
		"/_bridge/api/config",
		"/_bridge/api/reset",
		"/_bridge/api/detect-gateway",
		"/_bridge/api/logs/clear",
	} {
		for _, tc := range []struct {
			name, origin, sfs, ct string
			want                  int
		}{
			{"missing origin", "", "same-origin", "application/json", http.StatusForbidden},
			{"proxy origin", "http://localhost:8080", "same-site", "application/json", http.StatusForbidden},
			{"external origin", "https://evil.example", "cross-site", "application/json", http.StatusForbidden},
			{"cross-site sec-fetch-site", "http://localhost:8180", "cross-site", "application/json", http.StatusForbidden},
			{"same-site sec-fetch-site", "http://localhost:8180", "same-site", "application/json", http.StatusForbidden},
		} {
			req := httptest.NewRequest(http.MethodPost, "http://localhost:8180"+endpoint, strings.NewReader(`{"target":"192.168.1.2"}`))
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.sfs != "" {
				req.Header.Set("Sec-Fetch-Site", tc.sfs)
			}
			req.Header.Set("Content-Type", tc.ct)
			res := httptest.NewRecorder()
			controlH.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Errorf("%s (%s): status = %d, want %d", endpoint, tc.name, res.Code, tc.want)
			}
		}
	}

	// Reject text/plain simple CORS POST on /_bridge/api/config.
	plainReq := httptest.NewRequest(http.MethodPost, "http://localhost:8180/_bridge/api/config", strings.NewReader(`{"target":"192.168.1.2"}`))
	plainReq.Header.Set("Origin", "http://localhost:8180")
	plainReq.Header.Set("Sec-Fetch-Site", "same-origin")
	plainReq.Header.Set("Content-Type", "text/plain")
	plainRes := httptest.NewRecorder()
	controlH.ServeHTTP(plainRes, plainReq)
	if plainRes.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain POST /config status = %d, want 415", plainRes.Code)
	}

	// Reject self-targeting proxyPort and controlPort in UpdateTarget & /_bridge/api/config.
	for _, selfTarget := range []string{"127.0.0.1:8080", "localhost:8180", "[::1]:8080", "0.0.0.0:8180"} {
		selfReq := httptest.NewRequest(http.MethodPost, "http://localhost:8180/_bridge/api/config",
			strings.NewReader(fmt.Sprintf(`{"target":%q}`, selfTarget)))
		selfReq.Header.Set("Origin", "http://localhost:8180")
		selfReq.Header.Set("Sec-Fetch-Site", "same-origin")
		selfReq.Header.Set("Content-Type", "application/json")
		selfRes := httptest.NewRecorder()
		controlH.ServeHTTP(selfRes, selfReq)
		if selfRes.Code != http.StatusBadRequest || !strings.Contains(selfRes.Body.String(), `"address_self_blocked"`) {
			t.Errorf("self target %q: status=%d body=%s", selfTarget, selfRes.Code, selfRes.Body.String())
		}
	}
}

func TestStaticCacheSafetyAndInvalidation(t *testing.T) {
	var upstreamHits atomic.Int32
	var mode atomic.Value
	mode.Store("no-store")

	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := upstreamHits.Add(1)
		m := mode.Load().(string)
		switch m {
		case "no-store":
			w.Header().Set("Cache-Control", "no-store")
		case "private":
			w.Header().Set("Cache-Control", "private, max-age=60")
		case "pragma":
			w.Header().Set("Pragma", "no-cache")
		case "set-cookie":
			w.Header().Set("Set-Cookie", "Tok=1")
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = fmt.Fprintf(w, "var hit=%d;", n)
	}))
	defer upstreamSrv.Close()

	plane := newBoundControlPlane(t, Options{InitialTarget: upstreamSrv.URL})
	srv := plane.server
	proxyTs := plane.proxy

	for _, tc := range []string{"no-store", "private", "pragma", "set-cookie"} {
		mode.Store(tc)
		before := upstreamHits.Load()
		for i := 0; i < 2; i++ {
			resp, err := http.Get(proxyTs.URL + "/app.js")
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if cc := resp.Header.Get("Cache-Control"); strings.Contains(cc, "public, max-age=3600") {
				t.Fatalf("mode %s injected public Cache-Control: %q", tc, cc)
			}
		}
		if got := upstreamHits.Load() - before; got != 2 {
			t.Fatalf("mode %s should not cache static response (upstream hits=%d, want 2)", tc, got)
		}
	}

	// Authorization header must also bypass static cache.
	mode.Store("cacheable")
	beforeAuth := upstreamHits.Load()
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, proxyTs.URL+"/auth.js", nil)
		req.Header.Set("Authorization", "Basic YWRtaW46c2VjcmV0")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if cc := resp.Header.Get("Cache-Control"); strings.Contains(cc, "public, max-age=3600") {
			t.Fatalf("Authorization request injected public Cache-Control: %q", cc)
		}
	}
	if got := upstreamHits.Load() - beforeAuth; got != 2 {
		t.Fatalf("Authorization requests must not be cached (hits=%d)", got)
	}

	// Cacheable asset caches on second request, and clears on preset change or port/scheme change.
	resp1, _ := http.Get(proxyTs.URL + "/cached.js")
	_ = resp1.Body.Close()
	if srv.Snapshot().CachedAssets != 1 {
		t.Fatalf("expected 1 cached asset, got %d", srv.Snapshot().CachedAssets)
	}
	if err := srv.UpdateTarget(upstreamSrv.URL, "ilo"); err != nil {
		t.Fatal(err)
	}
	if srv.Snapshot().CachedAssets != 0 {
		t.Fatalf("expected staticCache cleared on preset change")
	}

	resp2, _ := http.Get(proxyTs.URL + "/cached.js")
	_ = resp2.Body.Close()
	if srv.Snapshot().CachedAssets != 1 {
		t.Fatalf("expected 1 cached asset before port/scheme change")
	}
	ep := upstream.ParseEndpoint(upstreamSrv.URL)
	if err := srv.UpdateTarget(fmt.Sprintf("https://%s:%d", ep.BareHost(), ep.Port), ""); err != nil {
		t.Fatal(err)
	}
	if srv.Snapshot().CachedAssets != 0 {
		t.Fatalf("expected staticCache cleared on scheme change even with same host and port")
	}
	// Also verify port change (using a distinct unused port) clears cache.
	_ = srv.UpdateTarget(upstreamSrv.URL, "")
	resp4, _ := http.Get(proxyTs.URL + "/cached2.js")
	_ = resp4.Body.Close()
	if srv.Snapshot().CachedAssets != 1 {
		t.Fatalf("expected 1 cached asset before port change")
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	altPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	if err := srv.UpdateTarget(fmt.Sprintf("http://%s:%d", ep.BareHost(), altPort), ""); err != nil {
		t.Fatal(err)
	}
	if srv.Snapshot().CachedAssets != 0 {
		t.Fatalf("expected staticCache cleared on port change even with same BareHost")
	}
}

func TestQueryRedactionInLogsAndHostBoundaryRewrite(t *testing.T) {
	// Host boundary rewriting must never corrupt 192.168.1.10 or 127.0.0.10.
	html := `<a href="http://192.168.1.1/ok">ok</a><a href="http://192.168.1.10/trap">trap</a>` +
		`<script>var a="https:\/\/192.168.1.1\/api";var b="https:\/\/192.168.1.1.evil.com\/x";var c='//192.168.1.1-bad/x';</script>`
	rewritten := RewriteResponseBody(html, "192.168.1.1", 8080, "localhost")
	for _, mustStay := range []string{
		"http://192.168.1.10/trap",
		`https:\/\/192.168.1.1.evil.com\/x`,
		"//192.168.1.1-bad/x",
		"http://localhost:8080/ok",
		`http:\/\/localhost:8080\/api`,
	} {
		if !strings.Contains(rewritten, mustStay) {
			t.Errorf("RewriteResponseBody missing %q in:\n%s", mustStay, rewritten)
		}
	}

	if got := RewriteRedirectLocation("https://127.0.0.10/trap", "127.0.0.1", 8080, "localhost", false); got != "https://127.0.0.10/trap" {
		t.Errorf("RewriteRedirectLocation corrupted prefix host: %q", got)
	}
	if got := RewriteRedirectLocation("https://192.168.1.1@evil.com/trap", "192.168.1.1", 8080, "localhost", false); got != "https://192.168.1.1@evil.com/trap" {
		t.Errorf("RewriteRedirectLocation corrupted userinfo URL: %q", got)
	}

	// Query strings must be redacted in [METHOD], [FWD], and [REDIRECT] logs.
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.Host+"/done?session=supersecret", http.StatusFound)
	}))
	defer upstreamSrv.Close()
	// Fix redirect URL scheme in handler via a quick custom handler.
	upstreamSrv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://"+r.Host+"/done?session=supersecret", http.StatusFound)
	})

	plane := newBoundControlPlane(t, Options{InitialTarget: upstreamSrv.URL})
	srv := plane.server
	proxyTs := plane.proxy

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	res, err := client.Get(proxyTs.URL + "/login?password=topsecretvalue")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	joinedLogs := strings.Join(srv.Snapshot().Logs, "\n")
	if strings.Contains(joinedLogs, "topsecretvalue") || strings.Contains(joinedLogs, "supersecret") {
		t.Fatalf("logs leaked query secret:\n%s", joinedLogs)
	}
	if !strings.Contains(joinedLogs, "[GET] /login?<redacted>") ||
		!strings.Contains(joinedLogs, "[FWD] -> ") ||
		!strings.Contains(joinedLogs, "[REDIRECT] ") {
		t.Fatalf("expected redacted diagnostic logs, got:\n%s", joinedLogs)
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
