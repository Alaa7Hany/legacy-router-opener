package bridgeui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alaa/legacyrouteropener/internal/upstream"
)

func TestSelectLanguage(t *testing.T) {
	for _, tc := range []struct{ explicit, accepted, want string }{
		{"", "", "en"},
		{"", "de-DE,fr", "en"},
		{"", "fr, ar-EG;q=0.8, en;q=0.5", "ar"},
		{"", "AR, en;q=0.8", "ar"},
		{"", "en-GB, ar;q=0.5", "en"},
		{"", "en;q=0.2, ar;q=0.9", "ar"},
		{"", "ar;q=0, en;q=0.5", "en"},
		{"", "ar;q=invalid, en;q=0.5", "en"},
		{"en", "ar-EG", "en"},
		{"ar", "en", "ar"},
		{"unsupported", "ar", "ar"},
	} {
		if got := SelectLanguage(tc.explicit, tc.accepted); got != tc.want {
			t.Errorf("SelectLanguage(%q, %q) = %q, want %q", tc.explicit, tc.accepted, got, tc.want)
		}
	}
}

func TestCatalogContainsTranslationsForBothLanguages(t *testing.T) {
	for _, locale := range []string{"en", "ar"} {
		for _, other := range messages {
			for key := range other {
				if strings.TrimSpace(Text(locale, key)) == "" {
					t.Errorf("%q missing translation for %q", locale, key)
				}
			}
		}
		for _, preset := range upstream.BuiltinPresets {
			for _, field := range []string{"_name", "_help"} {
				if Text(locale, "preset_"+preset.ID+field) == "" {
					t.Errorf("%q missing preset %q %s", locale, preset.ID, field)
				}
			}
		}
	}
}

func TestWriteErrorAndQuitAreSelfContainedAndThemeAware(t *testing.T) {
	var errBuf bytes.Buffer
	if err := WriteError(&errBuf, "ar", "192.168.1.1", "connection refused", "http://localhost:8180/_bridge"); err != nil {
		t.Fatalf("WriteError failed: %v", err)
	}
	errHTML := errBuf.String()
	for _, want := range []string{
		`href="http://localhost:8180/_bridge?edit=1&amp;lang=ar"`,
		`href="http://localhost:8180/_bridge?lang=ar"`,
		`function activateLanguage`,
		`retry-btn`,
	} {
		if !strings.Contains(errHTML, want) {
			t.Errorf("error.html missing %q", want)
		}
	}
	for _, unwanted := range []string{
		`/_bridge/assets/`,
		`/_bridge/api/config`,
		`export function`,
		`import { activateLanguage }`,
	} {
		if strings.Contains(errHTML, unwanted) {
			t.Errorf("error.html should not contain %q", unwanted)
		}
	}

	var quitBuf bytes.Buffer
	if err := WriteQuit(&quitBuf, "ar", "http://127.0.0.1:8180/_bridge?lang=ar", "tok123"); err != nil {
		t.Fatalf("WriteQuit failed: %v", err)
	}
	quitHTML := quitBuf.String()
	if !strings.Contains(quitHTML, `<meta name="color-scheme" content="light dark">`) {
		t.Errorf("quit.html missing color-scheme meta tag")
	}
	if !strings.Contains(quitHTML, `href="http://127.0.0.1:8180/_bridge?lang=ar"`) {
		t.Errorf("quit.html missing Cancel link preserving lang")
	}

	if !strings.Contains(bridgeCSS, "white-space: normal;") || !strings.Contains(bridgeCSS, "overflow-wrap: anywhere;") {
		t.Errorf("bridge.css .tls-line should wrap cleanly on narrow viewports")
	}
}

func TestWriteBridgeFooterLinksRenderWithAndWithoutQuitURL(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lang     string
		quitURL  string
		wantQuit bool
		wantText string
	}{
		{"without quit URL (en)", "en", "", false, "Website"},
		{"with quit URL (ar)", "ar", "/quit", true, "الموقع"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteBridge(&buf, tc.lang, "192.168.1.1", "192.168.1.1", tc.quitURL, struct{}{}); err != nil {
				t.Fatalf("WriteBridge failed: %v", err)
			}
			html := buf.String()
			for _, want := range []string{
				`href="https://3laa.me/legacy-router-opener/" target="_blank" rel="noopener noreferrer"`,
				`href="https://github.com/Alaa7Hany/legacy-router-opener" target="_blank" rel="noopener noreferrer"`,
				`<span data-message="footer_website">` + tc.wantText + `</span>`,
				`<span data-message="footer_source">GitHub</span>`,
				`<circle cx="12" cy="12" r="9"/>`,
				`class="footer-icon"`,
			} {
				if !strings.Contains(html, want) {
					t.Errorf("WriteBridge missing %q", want)
				}
			}
			hasQuit := strings.Contains(html, `id="quit-link"`)
			if hasQuit != tc.wantQuit {
				t.Errorf("WriteBridge hasQuit = %t, want %t", hasQuit, tc.wantQuit)
			}
		})
	}
}
