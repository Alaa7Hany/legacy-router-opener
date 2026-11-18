package proxy

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/alaa/legacyrouteropener/internal/bridgeui"
	"github.com/alaa/legacyrouteropener/internal/upstream"
)

const (
	maxCachedAssets     = 128
	maxCachedAssetBytes = 512 * 1024
	maxRequestBodyBytes = 64 * 1024 * 1024
)

var (
	strippedResponseHeaders = map[string]bool{
		"content-length":                      true,
		"content-encoding":                    true,
		"set-cookie":                          true,
		"transfer-encoding":                   true,
		"connection":                          true,
		"keep-alive":                          true,
		"x-frame-options":                     true,
		"content-security-policy":             true,
		"content-security-policy-report-only": true,
		"x-content-security-policy":           true,
		"x-webkit-csp":                        true,
		"cross-origin-embedder-policy":        true,
		"cross-origin-opener-policy":          true,
		"cross-origin-resource-policy":        true,
		"x-content-type-options":              true,
		"strict-transport-security":           true,
	}

	staticCacheStrippedHeaders = map[string]bool{
		"cache-control": true,
		"pragma":        true,
		"expires":       true,
	}
)

type cachedAsset struct {
	mimeType string
	body     []byte
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	target := s.target
	preset := s.preset
	proxyPort := s.proxyPort
	s.mu.RUnlock()

	targetPath := r.URL.RequestURI()
	if targetPath == "" {
		targetPath = "/"
	}
	effectiveProxyHost := resolveEffectiveProxyHost(r.Host)

	isStatic := r.Method == http.MethodGet && IsStaticAsset(targetPath)
	cacheKey := fmt.Sprintf("%s|%s|%s", target, effectiveProxyHost, targetPath)
	if isStatic {
		s.mu.RLock()
		cached, hit := s.staticCache[cacheKey]
		s.mu.RUnlock()
		if hit {
			s.appendLog(fmt.Sprintf("[CACHE] %s (%dB)", r.URL.Path, len(cached.body)))
			w.Header().Set("Content-Type", cached.mimeType)
			w.Header().Set("Content-Length", strconv.Itoa(len(cached.body)))
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Header().Set("X-Legacy-Router-Proxy", "1")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(cached.body)
			return
		}
	}

	s.appendLog(fmt.Sprintf("[%s] %s", r.Method, targetPath))

	var reqBody []byte
	if r.Body != nil {
		bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
		if err != nil {
			s.appendLog(fmt.Sprintf("[ERR] Request read error: %v", err))
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		reqBody = bodyBytes
	}

	jar := s.activeJar()
	mergedCookies := jar.GetMergedCookieHeader(r.Header.Get("Cookie"))

	s.appendLog(fmt.Sprintf("[FWD] -> %s%s", target, targetPath))
	resp, usedEndpoint, err := s.client.SendRequest(r.Context(), target, preset, upstream.UpstreamRequest{
		Method:  r.Method,
		Path:    targetPath,
		Headers: r.Header,
		Body:    reqBody,
	}, mergedCookies)
	if err != nil {
		s.appendLog(fmt.Sprintf("[ERR] Router connection failed: %v", err))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Legacy-Router-Proxy", "1")
		w.WriteHeader(http.StatusBadGateway)
		_ = bridgeui.WriteError(w, target, err.Error())
		return
	}

	setCookies := resp.Headers.Values("Set-Cookie")
	if len(setCookies) > 0 {
		if saved := jar.SaveCookiesFromHeaders(setCookies); len(saved) > 0 {
			s.appendLog(fmt.Sprintf("[COOKIE] Saved: %s", strings.Join(saved, "; ")))
		}
	}

	computedMime := GetAccurateMimeType(targetPath, resp.Headers, resp.Body)
	finalBytes := resp.Body
	if IsTransformableText(targetPath, computedMime, resp.Body) && len(resp.Body) > 0 {
		rewritten := RewriteResponseBody(string(resp.Body), usedEndpoint.Host, proxyPort, effectiveProxyHost)
		finalBytes = []byte(rewritten)
	}

	if isStatic &&
		resp.StatusCode == http.StatusOK &&
		len(setCookies) == 0 &&
		len(finalBytes) > 0 &&
		len(finalBytes) <= maxCachedAssetBytes {
		s.mu.Lock()
		if len(s.staticCache) < maxCachedAssets {
			s.staticCache[cacheKey] = cachedAsset{mimeType: computedMime, body: finalBytes}
		}
		s.mu.Unlock()
	}

	outHeaders := w.Header()
	for headerName, values := range resp.Headers {
		lower := strings.ToLower(headerName)
		if lower == "location" {
			loc := ""
			if len(values) > 0 {
				loc = values[0]
			}
			rewrittenLoc := RewriteRedirectLocation(loc, usedEndpoint.Host, proxyPort, effectiveProxyHost, preset.RewriteSingleLabelHost)
			outHeaders.Set("Location", rewrittenLoc)
			s.appendLog(fmt.Sprintf("[REDIRECT] %s -> %s", loc, rewrittenLoc))
			continue
		}
		if strippedResponseHeaders[lower] || (isStatic && staticCacheStrippedHeaders[lower]) {
			continue
		}
		if lower == "content-type" {
			continue
		}
		for _, v := range values {
			outHeaders.Add(headerName, v)
		}
	}

	for _, sc := range setCookies {
		if cleaned := CleanSetCookieHeader(sc); cleaned != "" {
			outHeaders.Add("Set-Cookie", cleaned)
		}
	}

	if isStatic && resp.StatusCode == http.StatusOK {
		outHeaders.Set("Cache-Control", "public, max-age=3600")
	}

	outHeaders.Set("Content-Type", computedMime)
	outHeaders.Set("Content-Length", strconv.Itoa(len(finalBytes)))
	outHeaders.Set("X-Legacy-Router-Proxy", "1")

	s.appendLog(fmt.Sprintf("[%d] %s", resp.StatusCode, computedMime))
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead && len(finalBytes) > 0 {
		_, _ = w.Write(finalBytes)
	}
}

func resolveEffectiveProxyHost(hostHeader string) string {
	trimmed := strings.TrimSpace(hostHeader)
	if trimmed == "" {
		return "localhost"
	}
	host := trimmed
	if strings.HasPrefix(trimmed, "[") {
		if idx := strings.IndexByte(trimmed, ']'); idx >= 0 {
			host = trimmed[:idx+1]
		}
	} else if h, _, err := net.SplitHostPort(trimmed); err == nil {
		host = h
	}
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "[::1]":
		return host
	default:
		return "localhost"
	}
}
