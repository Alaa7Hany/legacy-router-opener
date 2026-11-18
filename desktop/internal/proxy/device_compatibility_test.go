package proxy

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alaa/legacyrouteropener/internal/upstream"
)

const simulatedSessionID = "fixture-session"

func TestDesktopPresetsPreserveAuthenticatedNavigation(t *testing.T) {
	// These fixtures exercise browser flows against simulated web admin pages.
	// They represent connection and redirect behaviors rather than specific firmware.
	fixtures := map[string]struct {
		tlsVersion   uint16
		redirectHost string
	}{
		"auto":        {tls.VersionTLS10, ""},
		"huawei-zte":  {tls.VersionTLS10, ""},
		"idrac":       {tls.VersionTLS10, "fixture-idrac"},
		"ilo":         {tls.VersionTLS12, "fixture-ilo"},
		"apc":         {tls.VersionTLS12, ""},
		"cisco-hp":    {0, "fixture-switch"},
		"printer-nas": {0, ""},
	}

	for _, preset := range upstream.BuiltinPresets {
		t.Run(preset.ID, func(t *testing.T) {
			fixture, ok := fixtures[preset.ID]
			if !ok {
				t.Fatalf("add a simulated device for preset %q", preset.ID)
			}
			device := simulatedAdminPage(t, fixture.tlsVersion, fixture.redirectHost)
			bridge := NewServer(Options{InitialTarget: device.URL})
			local := httptest.NewServer(bridge)
			defer local.Close()
			bridge.SetProxyPort(local.Listener.Addr().(*net.TCPAddr).Port)

			// The client has no cookie jar, as if it dropped the device's login cookie.
			client := local.Client()
			client.Timeout = 3 * time.Second
			client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
				if req.URL.Scheme != "http" || req.URL.Host != local.Listener.Addr().String() {
					return fmt.Errorf("device redirect escaped the local bridge: %s", req.URL)
				}
				return nil
			}

			config, err := json.Marshal(map[string]string{"target": device.URL, "preset": preset.ID})
			if err != nil {
				t.Fatal(err)
			}
			res, err := client.Post(local.URL+"/_bridge/api/config", "application/json", strings.NewReader(string(config)))
			body := requireDeviceResponse(t, res, err, http.StatusOK)
			var state StateSnapshot
			if err := json.Unmarshal([]byte(body), &state); err != nil || state.Preset != preset.ID {
				t.Fatalf("could not select preset %q (selected %q, error %v)", preset.ID, state.Preset, err)
			}

			res, err = client.Get(local.URL + "/settings")
			requireDeviceResponse(t, res, err, http.StatusUnauthorized)

			res, err = client.PostForm(local.URL+"/login", url.Values{"password": {"fixture-password"}})
			body = requireDeviceResponse(t, res, err, http.StatusOK)
			if res.Request.URL.Path != "/settings" || !strings.Contains(body, "<h1>Device settings</h1>") {
				t.Fatalf("login did not reach the settings page: %s", res.Request.URL)
			}

			res, err = client.Get(local.URL + "/settings/network")
			body = requireDeviceResponse(t, res, err, http.StatusOK)
			if !strings.Contains(body, "<h1>Network settings</h1>") {
				t.Fatalf("could not open the next settings page: %q", body)
			}

			res, err = client.Get(local.URL + "/_bridge/api/state")
			body = requireDeviceResponse(t, res, err, http.StatusOK)
			if strings.Contains(body, simulatedSessionID) {
				t.Fatal("connection details exposed a session cookie value")
			}
			if !strings.Contains(body, "[COOKIE] Session saved") {
				t.Fatal("connection details omitted the session-saved message")
			}

			res, err = client.Post(local.URL+"/_bridge/api/reset", "application/json", nil)
			requireDeviceResponse(t, res, err, http.StatusOK)
			res, err = client.Get(local.URL + "/settings/network")
			requireDeviceResponse(t, res, err, http.StatusUnauthorized)
		})
	}
}

func simulatedAdminPage(t *testing.T, tlsVersion uint16, redirectHost string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("password") != "fixture-password" {
			http.Error(w, "incorrect password", http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: "device_session", Value: simulatedSessionID, Path: "/login",
			Secure: true, HttpOnly: true, SameSite: http.SameSiteNoneMode,
		})
		host := redirectHost
		if host == "" {
			host = r.Host
		}
		http.Redirect(w, r, "https://"+host+"/settings", http.StatusSeeOther)
	})
	for path, heading := range map[string]string{
		"/settings":         "<h1>Device settings</h1>",
		"/settings/network": "<h1>Network settings</h1>",
	} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("device_session")
			if err != nil || cookie.Value != simulatedSessionID {
				http.Error(w, "sign in first", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, heading)
		})
	}

	device := httptest.NewUnstartedServer(mux)
	if tlsVersion == 0 {
		device.Start()
	} else {
		device.TLS = &tls.Config{
			MinVersion: tlsVersion, MaxVersion: tlsVersion,
			CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA},
		}
		device.StartTLS()
	}
	t.Cleanup(device.Close)
	return device
}

func requireDeviceResponse(t *testing.T, res *http.Response, err error, wantStatus int) string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != wantStatus {
		t.Fatalf("%s %s returned %d, want %d: %s", res.Request.Method, res.Request.URL, res.StatusCode, wantStatus, body)
	}
	return string(body)
}
