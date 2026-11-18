package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestQuitRequiresHostOriginAndToken(t *testing.T) {
	const controlPort = 12345
	const host = "127.0.0.1:12345"
	stopped := false
	handler := newQuitHandler(controlPort, func() { stopped = true })
	req := httptest.NewRequest(http.MethodGet, "http://"+host+quitPath+"?lang=ar", nil)
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, req)
	if page.Code != http.StatusOK ||
		!strings.Contains(page.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
		!strings.Contains(page.Body.String(), `<meta name="color-scheme" content="light dark">`) ||
		!strings.Contains(page.Body.String(), handler.token) ||
		!strings.Contains(page.Body.String(), "http://"+host+"/_bridge?lang=ar") || stopped {
		t.Fatal("GET should show a protected confirmation preserving loopback host and lang")
	}

	for _, tc := range []struct {
		name, host, origin, sfs, token string
		status                         int
	}{
		{"evil rebinding host", "evil.example:12345", "http://" + host, "same-origin", handler.token, http.StatusNotFound},
		{"wrong loopback port", "localhost:8080", "http://" + host, "same-origin", handler.token, http.StatusNotFound},
		{"mismatched loopback origin", host, "http://localhost:12345", "same-origin", handler.token, http.StatusForbidden},
		{"proxy origin", host, "http://127.0.0.1:8080", "same-site", handler.token, http.StatusForbidden},
		{"external origin", host, "https://example.org", "cross-site", handler.token, http.StatusForbidden},
		{"cross-site sec-fetch-site", host, "http://" + host, "cross-site", handler.token, http.StatusForbidden},
		{"missing origin", host, "", "same-origin", handler.token, http.StatusForbidden},
		{"invalid token", host, "http://" + host, "same-origin", "different-token", http.StatusForbidden},
		{"authorized", host, "http://" + host, "same-origin", handler.token, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+quitPath,
				strings.NewReader(url.Values{"token": {tc.token}}.Encode()))
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.sfs != "" {
				req.Header.Set("Sec-Fetch-Site", tc.sfs)
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			result := httptest.NewRecorder()
			handler.ServeHTTP(result, req)
			if result.Code != tc.status || stopped != (tc.status == http.StatusOK) {
				t.Fatalf("status = %d, stopped = %t, want %d", result.Code, stopped, tc.status)
			}
			if stopped && !strings.Contains(result.Body.String(), "Router Page Opener stopped") {
				t.Fatal("response omitted the stopped message")
			}
		})
	}
}

func TestQuitClosesEventStreamAndListeners(t *testing.T) {
	available, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	portNumber := available.Addr().(*net.TCPAddr).Port
	available.Close()

	// Use an unreachable local port so tests never touch a real LAN router.
	closedTarget, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedTargetAddr := closedTarget.Addr().String()
	closedTarget.Close()

	app, opener, err := startApp(launchOptions{port: portNumber, target: "http://" + closedTargetAddr, noBrowser: true})
	if err != nil || app == nil {
		t.Fatalf("start app = (%v, %v)", app, err)
	}
	stopped := make(chan struct{})
	go func() {
		select {
		case <-app.quitRequested:
		case <-t.Context().Done():
		}
		app.shutdown()
		close(stopped)
	}()

	client := &http.Client{
		Timeout: 4 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	fetch := func(address string) string {
		t.Helper()
		resp, err := client.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s returned %d: %v", address, resp.StatusCode, err)
		}
		return string(body)
	}

	// Verify proxy origin is distinct from control origin and cannot reach /quit.
	proxyPingResp, err := client.Get(fmt.Sprintf("http://localhost:%d/_bridge/api/ping", portNumber))
	if err != nil {
		t.Fatal(err)
	}
	_ = proxyPingResp.Body.Close()
	controlPort := proxyPingResp.Header.Get(controlPortField)
	openerURL, _ := url.Parse(opener)
	if controlPort == "" || controlPort == fmt.Sprintf("%d", portNumber) || openerURL.Port() != controlPort {
		t.Fatalf("expected distinct control port matching opener URL %q, got header=%q", opener, controlPort)
	}

	proxyQuitResp, err := client.Get(fmt.Sprintf("http://localhost:%d/quit", portNumber))
	if err != nil {
		t.Fatal(err)
	}
	proxyQuitBody, _ := io.ReadAll(proxyQuitResp.Body)
	_ = proxyQuitResp.Body.Close()
	if proxyQuitResp.StatusCode != http.StatusBadGateway || strings.Contains(string(proxyQuitBody), `action="/quit`) {
		t.Fatalf("/quit must not be reachable on proxy origin (status=%d)", proxyQuitResp.StatusCode)
	}

	page := fetch(opener)
	if !strings.Contains(page, `id="quit-link" class="link-btn" href="/quit?lang=en"`) {
		t.Fatal("opener omitted relative Quit link")
	}
	controlHost := "localhost:" + controlPort
	origin := "http://" + controlHost
	quitURL := origin + quitPath + "?lang=ar"
	confirmation := fetch(quitURL)
	if !strings.Contains(confirmation, `<html lang="ar" dir="rtl">`) ||
		!strings.Contains(confirmation, `action="/quit?lang=ar"`) ||
		!strings.Contains(confirmation, `href="http://`+controlHost+`/_bridge?lang=ar"`) ||
		!strings.Contains(confirmation, "هل تريد إغلاق التطبيق؟") {
		t.Fatal("confirmation did not carry the selected language and Cancel URL")
	}
	_, tokenField, ok := strings.Cut(confirmation, `name="token" value="`)
	token, _, _ := strings.Cut(tokenField, `"`)
	if !ok || token == "" {
		t.Fatal("confirmation omitted its token")
	}

	stream, err := client.Get(opener + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	watchReq, err := http.NewRequest(http.MethodGet, origin+quitPath+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	watchReq.Header.Set("Sec-Fetch-Site", "same-origin")
	confirmationStream, err := client.Do(watchReq)
	if err != nil {
		t.Fatal(err)
	}
	defer confirmationStream.Body.Close()
	if confirmationStream.StatusCode != http.StatusOK ||
		confirmationStream.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal("the confirmation page could not start watching its tab")
	}
	req, err := http.NewRequest(http.MethodPost, quitURL, strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", origin)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	result, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(result.Body)
	result.Body.Close()
	if readErr != nil || result.StatusCode != http.StatusOK || !strings.Contains(string(body), "تم إغلاق التطبيق") {
		t.Fatalf("quit response = %d, error = %v", result.StatusCode, readErr)
	}
	if _, err := io.Copy(io.Discard, stream.Body); err != nil {
		t.Fatalf("event stream did not close cleanly: %v", err)
	}
	if _, err := io.Copy(io.Discard, confirmationStream.Body); err != nil {
		t.Fatalf("confirmation event stream did not close cleanly: %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown stalled with an active event stream")
	}
	for _, host := range []string{fmt.Sprintf("127.0.0.1:%d", portNumber), "127.0.0.1:" + controlPort} {
		conn, err := net.DialTimeout("tcp", host, time.Second)
		if err == nil {
			conn.Close()
			t.Errorf("listener still reachable at %s", host)
		}
	}
}
