package upstream

import (
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
