package bridgeui

import (
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
