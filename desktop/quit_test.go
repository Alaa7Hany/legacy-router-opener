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
	const host = "127.0.0.1:12345"
	const opener = "http://localhost:8080/_bridge"
	stopped := false
	handler := newQuitHandler(host, opener, func() { stopped = true })
	req := httptest.NewRequest(http.MethodGet, "http://"+host+quitPath, nil)
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, req)
	if page.Code != http.StatusOK ||
		!strings.Contains(page.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
		!strings.Contains(page.Body.String(), handler.token) ||
		!strings.Contains(page.Body.String(), opener) || stopped {
		t.Fatal("GET should show a protected confirmation and leave the app running")
	}

	for _, tc := range []struct {
		name, host, origin, token string
		status                    int
	}{
		{"different host", "localhost:12345", "http://" + host, handler.token, http.StatusNotFound},
		{"proxy origin", host, "http://127.0.0.1:8080", handler.token, http.StatusForbidden},
		{"external origin", host, "https://example.org", handler.token, http.StatusForbidden},
		{"missing origin", host, "", handler.token, http.StatusForbidden},
		{"invalid token", host, "http://" + host, "different-token", http.StatusForbidden},
		{"authorized", host, "http://" + host, handler.token, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+quitPath,
				strings.NewReader(url.Values{"token": {tc.token}}.Encode()))
			req.Header.Set("Origin", tc.origin)
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
	app, opener, err := startApp(launchOptions{port: portNumber, noBrowser: true})
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

	client := &http.Client{Timeout: 4 * time.Second}
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
	page := fetch(opener)
	_, link, ok := strings.Cut(page, `id="quit-link" class="link-btn" href="http://127.0.0.1:`)
	port, _, foundEnd := strings.Cut(link, quitPath+`?lang=en"`)
	if !ok || !foundEnd {
		t.Fatal("opener omitted Quit link")
	}
	quitHost := "127.0.0.1:" + port
	origin := "http://" + quitHost
	quitURL := origin + quitPath + "?lang=ar"
	confirmation := fetch(quitURL)
	if !strings.Contains(confirmation, `<html lang="ar" dir="rtl">`) ||
		!strings.Contains(confirmation, `action="/quit?lang=ar"`) ||
		!strings.Contains(confirmation, "هل تريد إغلاق التطبيق؟") {
		t.Fatal("confirmation did not carry the selected language into the form")
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
	openerURL, _ := url.Parse(opener)
	for _, host := range []string{fmt.Sprintf("127.0.0.1:%s", openerURL.Port()), quitHost} {
		conn, err := net.DialTimeout("tcp", host, time.Second)
		if err == nil {
			conn.Close()
			t.Errorf("listener still reachable at %s", host)
		}
	}
}
