package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
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

func TestValidateTarget(t *testing.T) {
	valid := []string{
		"192.168.1.1",
		" 192.168.1.1 ",
		"192.168.0.120:8443",
		"http://192.168.1.1",
		"https://192.168.1.1:80/login",
		"HTTP://10.0.0.1:8080/index.html",
		"[fe80::1]:8443",
		"::1",
		"idrac-r610",
		"router.local:8443",
	}
	for _, raw := range valid {
		if _, err := ValidateTarget(raw); err != nil {
			t.Errorf("ValidateTarget(%q) unexpectedly failed: %v", raw, err)
		}
	}

	invalid := []string{
		"",
		"   ",
		"bad host",
		"192.168.1.1\r\n",
		"192.168.1.1\n",
		"192.168.1.1\t",
		"192.168.1.1\x00",
		"javascript:alert(1)",
		"file:///etc/passwd",
		"ftp://192.168.1.1",
		"user@192.168.1.1",
		"http://user@192.168.1.1/",
		"<script>192.168.1.1</script>",
		`"192.168.1.1"`,
		"'192.168.1.1'",
		"192.168.1.1?token=1",
		"192.168.1.1#frag",
		"192.168.1.1:0",
		"192.168.1.1:70000",
		"192.168.1.1:abc",
		"192.168.1.1:+80",
		"192.168.1.1:",
		"999.999.999.999",
		"192.168.1",
		":8080",
		"http:///login",
		"-bad.local",
		"bad-.local",
		"bad..local",
		"[::1]:0",
		"[::1]:70000",
		"[not-ipv6]",
		"[192.168.1.1]",
	}
	for _, raw := range invalid {
		if ep, err := ValidateTarget(raw); err == nil {
			t.Errorf("ValidateTarget(%q) unexpectedly succeeded with %+v", raw, ep)
		}
	}
}

func TestClientBlocksOwnLoopbackPortsOnly(t *testing.T) {
	var blockedHits atomic.Int32
	blockedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blockedHits.Add(1)
		_, _ = io.WriteString(w, "should-not-be-reached")
	}))
	defer blockedSrv.Close()

	allowedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "tunnel-ok")
	}))
	defer allowedSrv.Close()

	blockedEP := ParseEndpoint(blockedSrv.URL)
	controlPort := blockedEP.Port + 100

	client := NewClient(4, nil)
	client.SetBlockedLoopbackPorts(blockedEP.Port, controlPort)

	for _, target := range []string{
		blockedSrv.URL,
		"http://localhost:" + strconv.Itoa(blockedEP.Port),
		"http://127.0.0.2:" + strconv.Itoa(blockedEP.Port),
		"http://[::1]:" + strconv.Itoa(controlPort),
		"http://0.0.0.0:" + strconv.Itoa(blockedEP.Port),
	} {
		_, _, err := client.SendRequest(context.Background(), target, FindPreset("auto"), UpstreamRequest{
			Method: http.MethodGet,
			Path:   "/",
		}, "")
		if !errors.Is(err, ErrSelfTargetBlocked) {
			t.Fatalf("SendRequest(%q) err = %v, want ErrSelfTargetBlocked", target, err)
		}
	}
	if blockedHits.Load() != 0 {
		t.Fatalf("blocked server received %d connections, want 0", blockedHits.Load())
	}

	resp, _, err := client.SendRequest(context.Background(), allowedSrv.URL, FindPreset("auto"), UpstreamRequest{
		Method: http.MethodGet,
		Path:   "/",
	}, "")
	if err != nil {
		t.Fatalf("SendRequest to non-blocked loopback port failed: %v", err)
	}
	if string(resp.Body) != "tunnel-ok" {
		t.Fatalf("Body = %q, want tunnel-ok", string(resp.Body))
	}
}

func TestRedactQuery(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/login?user=admin&pass=secret", "/login?<redacted>"},
		{"https://192.168.1.1/done?sid=123#frag", "https://192.168.1.1/done?<redacted>"},
		{"/html/main.html", "/html/main.html"},
	}
	for _, tc := range cases {
		if got := RedactQuery(tc.in); got != tc.want {
			t.Errorf("RedactQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
