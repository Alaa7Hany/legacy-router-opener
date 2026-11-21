package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const unreachableTarget = "http://127.0.0.1:1"

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

// launchBridge starts the Bridge through its real launch interface and waits
// until the control origin advertises its identity. It returns the launch
// result channel, a client and the control port discovered via ping.
func launchBridge(t *testing.T, port int) (<-chan error, *http.Client, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, launchOptions{port: port, target: unreachableTarget, noBrowser: true})
	}()

	client := &http.Client{
		Timeout: 4 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	controlPort := ""
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && controlPort == "" {
		resp, err := client.Get(fmt.Sprintf("http://localhost:%d/_bridge/api/ping", port))
		if err == nil {
			cp := resp.Header.Get(controlPortField)
			status := resp.StatusCode
			proxy := resp.Header.Get("X-Legacy-Router-Proxy")
			_ = resp.Body.Close()
			if status == http.StatusOK && proxy == "1" && cp != "" {
				controlPort = cp
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if controlPort == "" {
		t.Fatalf("bridge did not advertise a control port on %d", port)
	}
	return done, client, controlPort
}

func requireStop(t *testing.T, done <-chan error, within time.Duration, wantErr bool) {
	t.Helper()
	select {
	case err := <-done:
		if wantErr {
			if err == nil || !strings.Contains(err.Error(), "stopped unexpectedly") {
				t.Fatalf("expected an unexpected-stop error, got %v", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("bridge stopped with error: %v", err)
		}
	case <-time.After(within):
		t.Fatalf("bridge did not stop within %s", within)
	}
}

func fetchOK(t *testing.T, client *http.Client, req *http.Request) string {
	t.Helper()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s returned %d: %v", req.URL, resp.StatusCode, err)
	}
	return string(body)
}

func scrapeQuitToken(t *testing.T, page string) string {
	t.Helper()
	_, field, ok := strings.Cut(page, `name="token" value="`)
	token, _, _ := strings.Cut(field, `"`)
	if !ok || token == "" {
		t.Fatal("confirmation omitted its token")
	}
	return token
}

func TestQuitRequiresHostOriginAndToken(t *testing.T) {
	port := freeLoopbackPort(t)
	done, client, controlPort := launchBridge(t, port)
	host := "127.0.0.1:" + controlPort
	origin := "http://" + host
	postURL := origin + quitPath

	getReq, err := http.NewRequest(http.MethodGet, origin+quitPath+"?lang=ar", nil)
	if err != nil {
		t.Fatal(err)
	}
	getReq.Host = host
	pageResp, err := client.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(pageResp.Body)
	pageResp.Body.Close()
	if pageResp.StatusCode != http.StatusOK ||
		!strings.Contains(pageResp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
		!strings.Contains(string(page), `<meta name="color-scheme" content="light dark">`) ||
		!strings.Contains(string(page), "http://"+host+"/_bridge?lang=ar") {
		t.Fatal("GET should show a protected confirmation preserving loopback host and lang")
	}
	token := scrapeQuitToken(t, string(page))
	if !strings.Contains(string(page), token) {
		t.Fatal("confirmation omitted its token")
	}

	for _, tc := range []struct {
		name, host, origin, sfs, token string
		status                         int
	}{
		{"evil rebinding host", "evil.example:12345", origin, "same-origin", token, http.StatusNotFound},
		{"wrong loopback port", "localhost:8080", origin, "same-origin", token, http.StatusNotFound},
		{"mismatched loopback origin", host, "http://localhost:" + controlPort, "same-origin", token, http.StatusForbidden},
		{"proxy origin", host, "http://127.0.0.1:8080", "same-site", token, http.StatusForbidden},
		{"external origin", host, "https://example.org", "cross-site", token, http.StatusForbidden},
		{"cross-site sec-fetch-site", host, origin, "cross-site", token, http.StatusForbidden},
		{"missing origin", host, "", "same-origin", token, http.StatusForbidden},
		{"invalid token", host, origin, "same-origin", "different-token", http.StatusForbidden},
		{"authorized", host, origin, "same-origin", token, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, postURL,
				strings.NewReader(url.Values{"token": {tc.token}}.Encode()))
			if err != nil {
				t.Fatal(err)
			}
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.sfs != "" {
				req.Header.Set("Sec-Fetch-Site", tc.sfs)
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			result, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer result.Body.Close()
			body, _ := io.ReadAll(result.Body)
			if result.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", result.StatusCode, tc.status)
			}
			if tc.status == http.StatusOK && !strings.Contains(string(body), "Router Page Opener stopped") {
				t.Fatal("response omitted the stopped message")
			}
		})
	}
	requireStop(t, done, 3*time.Second, false)
}

func TestQuitClosesEventStreamAndListeners(t *testing.T) {
	port := freeLoopbackPort(t)
	done, client, controlPort := launchBridge(t, port)
	host := "127.0.0.1:" + controlPort
	origin := "http://" + host

	// Verify proxy origin is distinct from control origin and cannot reach /quit.
	proxyPingResp, err := client.Get(fmt.Sprintf("http://localhost:%d/_bridge/api/ping", port))
	if err != nil {
		t.Fatal(err)
	}
	_ = proxyPingResp.Body.Close()
	if got := proxyPingResp.Header.Get(controlPortField); got == "" || got == fmt.Sprintf("%d", port) {
		t.Fatalf("expected distinct control port, got %q", got)
	}
	proxyQuitResp, err := client.Get(fmt.Sprintf("http://localhost:%d/quit", port))
	if err != nil {
		t.Fatal(err)
	}
	proxyQuitBody, _ := io.ReadAll(proxyQuitResp.Body)
	_ = proxyQuitResp.Body.Close()
	if proxyQuitResp.StatusCode != http.StatusBadGateway || strings.Contains(string(proxyQuitBody), `action="/quit`) {
		t.Fatalf("/quit must not be reachable on proxy origin (status=%d)", proxyQuitResp.StatusCode)
	}

	openerURL := origin + "/_bridge"
	page := fetchOK(t, client, mustRequest(t, http.MethodGet, openerURL+"?lang=en", ""))
	if !strings.Contains(page, `id="quit-link" class="link-btn" href="/quit?lang=en"`) {
		t.Fatal("opener omitted relative Quit link")
	}

	quitURL := origin + quitPath + "?lang=ar"
	confirmation := fetchOK(t, client, mustRequest(t, http.MethodGet, quitURL, ""))
	if !strings.Contains(confirmation, `<html lang="ar" dir="rtl">`) ||
		!strings.Contains(confirmation, `action="/quit?lang=ar"`) ||
		!strings.Contains(confirmation, `href="http://`+host+`/_bridge?lang=ar"`) ||
		!strings.Contains(confirmation, "هل تريد إغلاق التطبيق؟") {
		t.Fatal("confirmation did not carry the selected language and Cancel URL")
	}
	token := scrapeQuitToken(t, confirmation)

	stream, err := client.Do(mustRequest(t, http.MethodGet, openerURL+"/events?opener=1", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	confirmationStream, err := client.Do(mustRequest(t, http.MethodGet, origin+quitPath+"/events", "same-origin"))
	if err != nil {
		t.Fatal(err)
	}
	defer confirmationStream.Body.Close()
	if confirmationStream.StatusCode != http.StatusOK ||
		confirmationStream.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal("the confirmation page could not start watching its tab")
	}

	post, err := http.NewRequest(http.MethodPost, quitURL, strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	post.Header.Set("Origin", origin)
	post.Header.Set("Sec-Fetch-Site", "same-origin")
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	result, err := client.Do(post)
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
	requireStop(t, done, 3*time.Second, false)
	for _, addr := range []string{fmt.Sprintf("127.0.0.1:%d", port), "127.0.0.1:" + controlPort} {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			t.Errorf("listener still reachable at %s", addr)
		}
	}
}

func TestLastOpenerTabCloseStopsCleanly(t *testing.T) {
	port := freeLoopbackPort(t)
	done, client, controlPort := launchBridge(t, port)

	stream, err := client.Do(mustRequest(t, http.MethodGet,
		"http://localhost:"+controlPort+"/_bridge/events?opener=1", "same-origin"))
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stream.Body).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "data: ") {
		t.Fatalf("opener stream did not start: %q (%v)", line, err)
	}
	_ = stream.Body.Close()

	requireStop(t, done, tabReconnectGrace+3*time.Second, false)
}

func TestServeFailureReportsError(t *testing.T) {
	port := freeLoopbackPort(t)
	app, _, err := startApp(launchOptions{port: port, target: unreachableTarget, noBrowser: true})
	if err != nil || app == nil {
		t.Fatalf("start app = (%v, %v)", app, err)
	}
	t.Cleanup(app.shutdown)

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/_bridge/api/ping", port))
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(app.process.controlListeners) == 0 {
		t.Fatal("bridge holds no control listener")
	}
	_ = app.process.controlListeners[0].Close()

	select {
	case stopErr := <-app.process.stopSignal():
		if stopErr == nil {
			t.Fatal("serve failure sent no error on the stop signal")
		}
		if reported := stopError(stopErr); reported == nil ||
			!strings.Contains(reported.Error(), "stopped unexpectedly") {
			t.Fatalf("unexpected stop report: %v", reported)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve failure did not reach the stop signal")
	}
}

func mustRequest(t *testing.T, method, address, secFetchSite string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	if secFetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", secFetchSite)
	}
	return req
}
