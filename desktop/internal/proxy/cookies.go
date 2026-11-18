package proxy

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	maxAgeZeroRegex = regexp.MustCompile(`(?i);\s*max-age\s*=\s*(?:0|-\d+)\b`)
	domainAttrRegex = regexp.MustCompile(`(?i);\s*domain=[^;]+`)
	secureAttrRegex = regexp.MustCompile(`(?i);\s*secure\b`)
	sameSiteRegex   = regexp.MustCompile(`(?i);\s*samesite=none\b`)
	pathAttrRegex   = regexp.MustCompile(`(?i)(?:^|;)\s*path=`)
)

// HuaweiInitialCookies pre-populates menu/language state expected by Huawei HG531/HG532 framesets.
var HuaweiInitialCookies = map[string]string{
	"FirstMenu":  "Admin_0",
	"SecondMenu": "Admin_0_0",
	"ThirdMenu":  "Admin_0_0_0",
	"Language":   "en",
}

// SessionCookieJar maintains server-side cookies per session, automatically re-seeding
// Huawei defaults when configured and tracking evicted cookie names so browsers can clear them.
type SessionCookieJar struct {
	mu           sync.RWMutex
	cookies      map[string]string
	knownKeys    map[string]struct{}
	seedHuawei   bool
}

// NewSessionCookieJar creates a SessionCookieJar optionally pre-seeded with Huawei menu cookies.
func NewSessionCookieJar(seedHuawei bool) *SessionCookieJar {
	jar := &SessionCookieJar{
		cookies:    make(map[string]string),
		knownKeys:  make(map[string]struct{}),
		seedHuawei: seedHuawei,
	}
	jar.applySeedLocked()
	return jar
}

func (j *SessionCookieJar) applySeedLocked() {
	if !j.seedHuawei {
		return
	}
	for k, v := range HuaweiInitialCookies {
		j.cookies[k] = v
		j.knownKeys[k] = struct{}{}
	}
}

// SetSeedHuawei updates whether Clear/Reset pre-seeds Huawei defaults and applies them immediately if enabled.
func (j *SessionCookieJar) SetSeedHuawei(enabled bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.seedHuawei = enabled
	if enabled {
		for k, v := range HuaweiInitialCookies {
			if _, exists := j.cookies[k]; !exists {
				j.cookies[k] = v
			}
			j.knownKeys[k] = struct{}{}
		}
	} else {
		for k, v := range HuaweiInitialCookies {
			if cur, exists := j.cookies[k]; exists && cur == v {
				delete(j.cookies, k)
			}
		}
	}
}

// SaveCookiesFromHeaders parses upstream Set-Cookie headers into the jar and returns newly saved key=value entries.
func (j *SessionCookieJar) SaveCookiesFromHeaders(setCookieHeaders []string) []string {
	if len(setCookieHeaders) == 0 {
		return nil
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	var saved []string
	for _, header := range setCookieHeaders {
		mainPart := strings.TrimSpace(strings.SplitN(header, ";", 2)[0])
		eqIdx := strings.IndexByte(mainPart, '=')
		if eqIdx <= 0 {
			continue
		}
		key := strings.TrimSpace(mainPart[:eqIdx])
		val := strings.TrimSpace(mainPart[eqIdx+1:])
		if key == "" {
			continue
		}
		j.knownKeys[key] = struct{}{}

		isExpired := maxAgeZeroRegex.MatchString(header)
		if isExpired || val == "" || strings.EqualFold(val, "deleted") {
			delete(j.cookies, key)
			continue
		}
		j.cookies[key] = val
		saved = append(saved, key+"="+val)
	}
	return saved
}

// GetMergedCookieHeader combines the server-side cookie jar with incoming browser cookies.
// Incoming browser cookies take precedence on key collisions, matching Android's SessionCookieJar.
func (j *SessionCookieJar) GetMergedCookieHeader(incomingBrowserCookies string) string {
	j.mu.RLock()
	merged := make(map[string]string, len(j.cookies)+4)
	for k, v := range j.cookies {
		merged[k] = v
	}
	j.mu.RUnlock()

	if strings.TrimSpace(incomingBrowserCookies) != "" {
		for _, part := range strings.Split(incomingBrowserCookies, ";") {
			eqIdx := strings.IndexByte(part, '=')
			if eqIdx <= 0 {
				continue
			}
			key := strings.TrimSpace(part[:eqIdx])
			val := strings.TrimSpace(part[eqIdx+1:])
			if key != "" {
				merged[key] = val
			}
		}
	}

	if len(merged) == 0 {
		return ""
	}

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+merged[k])
	}
	return strings.Join(parts, "; ")
}

// Snapshot returns a sorted copy of the currently stored jar cookies.
func (j *SessionCookieJar) Snapshot() map[string]string {
	j.mu.RLock()
	defer j.mu.RUnlock()

	out := make(map[string]string, len(j.cookies))
	for k, v := range j.cookies {
		out[k] = v
	}
	return out
}

// Clear resets the cookie jar (re-seeding Huawei defaults if enabled) and returns all previously
// observed cookie keys so callers can emit browser expiration headers.
func (j *SessionCookieJar) Clear() []string {
	j.mu.Lock()
	defer j.mu.Unlock()

	evictKeys := make([]string, 0, len(j.knownKeys))
	for k := range j.knownKeys {
		evictKeys = append(evictKeys, k)
	}
	sort.Strings(evictKeys)

	j.cookies = make(map[string]string)
	j.applySeedLocked()
	return evictKeys
}

// CleanSetCookieHeader strips Secure, Domain, and SameSite=None flags that break cookies
// on plain HTTP localhost and ensures Path=/ is present.
func CleanSetCookieHeader(cookieHeader string) string {
	cleaned := domainAttrRegex.ReplaceAllString(cookieHeader, "")
	cleaned = secureAttrRegex.ReplaceAllString(cleaned, "")
	cleaned = sameSiteRegex.ReplaceAllString(cleaned, "; SameSite=Lax")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return ""
	}
	if !pathAttrRegex.MatchString(cleaned) {
		cleaned += "; Path=/"
	}
	return cleaned
}
