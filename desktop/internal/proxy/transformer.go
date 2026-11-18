package proxy

import (
	"bytes"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
)

var (
	staticAssetExtensions = map[string]bool{
		".gif":  true,
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".ico":  true,
		".svg":  true,
		".css":  true,
		".js":   true,
		".res":  true,
	}

	extensionMimeTypes = map[string]string{
		".css":   "text/css; charset=utf-8",
		".js":    "application/javascript; charset=utf-8",
		".res":   "application/javascript; charset=utf-8",
		".html":  "text/html; charset=utf-8",
		".htm":   "text/html; charset=utf-8",
		".asp":   "text/html; charset=utf-8",
		".cgi":   "text/html; charset=utf-8",
		".png":   "image/png",
		".gif":   "image/gif",
		".jpg":   "image/jpeg",
		".jpeg":  "image/jpeg",
		".ico":   "image/x-icon",
		".svg":   "image/svg+xml",
		".json":  "application/json; charset=utf-8",
		".xml":   "text/xml; charset=utf-8",
		".jar":   "application/java-archive",
		".class": "application/java-vm",
		".cab":   "application/vnd.ms-cab-compressed",
		".woff":  "font/woff",
		".woff2": "font/woff2",
		".ttf":   "font/ttf",
		".eot":   "application/vnd.ms-fontobject",
		".bin":   "application/octet-stream",
		".rom":   "application/octet-stream",
	}

	transformableExtensions = map[string]bool{
		".html": true,
		".htm":  true,
		".asp":  true,
		".cgi":  true,
		".js":   true,
		".res":  true,
		".css":  true,
		".json": true,
		".xml":  true,
		".txt":  true,
	}

	singleLabelRedirectRegex = regexp.MustCompile(`(?i)^https?://([a-z0-9-]+)(:\d+)?(/.*)?$`)
)

// IsStaticAsset returns true for cacheable router static resources (.gif, .png, .css, .js, etc.).
func IsStaticAsset(requestPath string) bool {
	ext := cleanPathExt(requestPath)
	return staticAssetExtensions[ext]
}

// GetAccurateMimeType resolves the Content-Type prioritizing well-known embedded router extensions
// before falling back to the upstream Content-Type header or byte sniffing.
func GetAccurateMimeType(requestPath string, headers http.Header, body []byte) string {
	ext := cleanPathExt(requestPath)
	if mime, ok := extensionMimeTypes[ext]; ok {
		// Preserve upstream charset for text/html if the device specified one (e.g. windows-1256).
		if strings.HasPrefix(mime, "text/") || strings.HasPrefix(mime, "application/javascript") {
			if upstream := strings.TrimSpace(headers.Get("Content-Type")); upstream != "" && strings.Contains(strings.ToLower(upstream), "charset=") {
				return upstream
			}
		}
		return mime
	}

	if upstream := strings.TrimSpace(headers.Get("Content-Type")); upstream != "" {
		return upstream
	}

	if len(body) > 0 {
		sniffed := http.DetectContentType(body)
		if sniffed != "application/octet-stream" {
			return sniffed
		}
	}

	return "text/html; charset=utf-8"
}

// IsTransformableText decides whether a response body should undergo IP/URL string rewriting.
// It protects binary payloads (images, applets, firmware blobs, null-byte streams) from UTF-8 corruption.
func IsTransformableText(requestPath string, computedMimeType string, body []byte) bool {
	ext := cleanPathExt(requestPath)
	if transformableExtensions[ext] {
		return true
	}

	lower := strings.ToLower(computedMimeType)
	if strings.Contains(lower, "image/") ||
		strings.Contains(lower, "font/") ||
		strings.Contains(lower, "octet-stream") ||
		strings.Contains(lower, "java-archive") ||
		strings.Contains(lower, "zip") ||
		strings.Contains(lower, "pdf") {
		return false
	}

	if strings.Contains(lower, "html") ||
		strings.Contains(lower, "javascript") ||
		strings.Contains(lower, "json") ||
		strings.Contains(lower, "text") ||
		strings.Contains(lower, "xml") {
		return !containsNullBytes(body)
	}

	return false
}

// RewriteResponseBody replaces hardcoded router IP/port references and HTTPS URLs with the local proxy authority.
func RewriteResponseBody(body string, routerEndpoint string, proxyPort int, proxyHost string) string {
	if body == "" {
		return body
	}
	if proxyHost == "" {
		proxyHost = "localhost"
	}

	targetHost := ParseEndpoint(routerEndpoint).Host
	escapedHost := regexp.QuoteMeta(targetHost)
	proxyAuthority := fmt.Sprintf("%s:%d", proxyHost, proxyPort)

	// 1. Standard URLs: http://192.168.1.1:443 or https://192.168.1.1
	urlRegex := regexp.MustCompile(`(?i)https?://` + escapedHost + `(:\d+)?`)
	modified := urlRegex.ReplaceAllString(body, "http://"+proxyAuthority)

	// 2. JSON / inline JS escaped URLs: https:\/\/192.168.1.1
	escapedURLRegex := regexp.MustCompile(`(?i)https?:\\/\\/` + escapedHost + `(:\d+)?`)
	modified = escapedURLRegex.ReplaceAllString(modified, `http:\/\/`+proxyAuthority)

	// 3. Protocol-relative URLs preceded by quote or parenthesis: '//192.168.1.1:443' or (//192.168.1.1)
	protoRelRegex := regexp.MustCompile(`(?i)(['"\(])//` + escapedHost + `(:\d+)?`)
	modified = protoRelRegex.ReplaceAllString(modified, "${1}//"+proxyAuthority)

	// 4. Bare quoted router IP literals ('192.168.1.1' or "192.168.1.1:443")
	singleQuotedRegex := regexp.MustCompile(`'` + escapedHost + `(:\d+)?'`)
	modified = singleQuotedRegex.ReplaceAllString(modified, "'"+proxyAuthority+"'")

	doubleQuotedRegex := regexp.MustCompile(`"` + escapedHost + `(:\d+)?"`)
	modified = doubleQuotedRegex.ReplaceAllString(modified, `"`+proxyAuthority+`"`)

	return modified
}

// RewriteRedirectLocation rewrites upstream Location redirect headers pointing to the target device
// (or single-label internal BMC hostnames like https://idrac-abc123/login.html) back to localhost.
func RewriteRedirectLocation(location string, routerEndpoint string, proxyPort int, proxyHost string, rewriteSingleLabelHost bool) string {
	trimmed := strings.TrimSpace(location)
	if trimmed == "" {
		return trimmed
	}
	if proxyHost == "" {
		proxyHost = "localhost"
	}

	targetHost := ParseEndpoint(routerEndpoint).Host
	escapedHost := regexp.QuoteMeta(targetHost)
	proxyBase := fmt.Sprintf("http://%s:%d", proxyHost, proxyPort)

	urlRegex := regexp.MustCompile(`(?i)https?://` + escapedHost + `(:\d+)?`)
	if urlRegex.MatchString(trimmed) {
		return urlRegex.ReplaceAllString(trimmed, proxyBase)
	}

	protoRelRegex := regexp.MustCompile(`(?i)^//` + escapedHost + `(:\d+)?`)
	if protoRelRegex.MatchString(trimmed) {
		return protoRelRegex.ReplaceAllString(trimmed, proxyBase)
	}

	// Handle Dell iDRAC / HPE iLO redirects to internal non-FQDN hostnames (e.g., https://idrac-123/login.html).
	if rewriteSingleLabelHost {
		if m := singleLabelRedirectRegex.FindStringSubmatch(trimmed); len(m) >= 4 {
			host := m[1]
			rest := m[3]
			if !strings.EqualFold(host, "localhost") {
				return proxyBase + rest
			}
		}
	}

	return trimmed
}

func cleanPathExt(rawPath string) string {
	p := rawPath
	if idx := strings.IndexAny(p, "?#"); idx >= 0 {
		p = p[:idx]
	}
	return strings.ToLower(path.Ext(p))
}

func containsNullBytes(b []byte) bool {
	sampleLen := len(b)
	if sampleLen > 1024 {
		sampleLen = 1024
	}
	return bytes.IndexByte(b[:sampleLen], 0) >= 0
}
