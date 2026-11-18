package upstream

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const maxResponseBodyBytes = 64 * 1024 * 1024

var (
	errResponseTooLarge = errors.New("upstream response body exceeded 64MB limit")
	proxyAuthorityRegex = regexp.MustCompile(`(?i)https?://(127\.0\.0\.1|localhost|\[::1\])(:\d+)?`)

	strippedOutgoingHeaders = map[string]bool{
		"host":                      true,
		"referer":                   true,
		"origin":                    true,
		"accept":                    true,
		"accept-encoding":           true,
		"connection":                true,
		"keep-alive":                true,
		"proxy-connection":          true,
		"cookie":                    true,
		"content-length":            true,
		"content-type":              true,
		"if-modified-since":         true,
		"if-none-match":             true,
		"upgrade-insecure-requests": true,
	}
)

// UpstreamRequest represents a normalized browser request to forward upstream.
type UpstreamRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

// UpstreamResponse represents a raw response parsed leniently from the legacy device.
type UpstreamResponse struct {
	StatusCode int
	StatusText string
	Headers    http.Header
	Body       []byte
}

func buildRawHTTPRequest(endpoint Endpoint, req UpstreamRequest, cookieHeader string) []byte {
	var buf bytes.Buffer
	reqPath := req.Path
	if reqPath == "" {
		reqPath = "/"
	}
	if !strings.HasPrefix(reqPath, "/") {
		reqPath = "/" + reqPath
	}

	schemeAuthority := endpoint.URL()
	incomingReferer := req.Headers.Get("Referer")
	outgoingReferer := schemeAuthority + "/"
	if strings.TrimSpace(incomingReferer) != "" {
		outgoingReferer = proxyAuthorityRegex.ReplaceAllString(incomingReferer, schemeAuthority)
	}

	buf.WriteString(req.Method)
	buf.WriteByte(' ')
	buf.WriteString(reqPath)
	buf.WriteString(" HTTP/1.1\r\n")
	buf.WriteString("Host: ")
	buf.WriteString(endpoint.Authority())
	buf.WriteString("\r\n")
	buf.WriteString("Referer: ")
	buf.WriteString(outgoingReferer)
	buf.WriteString("\r\n")
	buf.WriteString("Origin: ")
	buf.WriteString(schemeAuthority)
	buf.WriteString("\r\n")
	buf.WriteString("Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8\r\n")
	buf.WriteString("Accept-Encoding: identity\r\n")
	buf.WriteString("Connection: close\r\n")

	for key, values := range req.Headers {
		if strippedOutgoingHeaders[strings.ToLower(key)] {
			continue
		}
		for _, val := range values {
			buf.WriteString(key)
			buf.WriteString(": ")
			buf.WriteString(val)
			buf.WriteString("\r\n")
		}
	}

	if cookieHeader != "" {
		buf.WriteString("Cookie: ")
		buf.WriteString(cookieHeader)
		buf.WriteString("\r\n")
	}

	if len(req.Body) > 0 {
		ct := req.Headers.Get("Content-Type")
		if ct == "" {
			ct = "application/x-www-form-urlencoded"
		}
		buf.WriteString("Content-Type: ")
		buf.WriteString(ct)
		buf.WriteString("\r\n")
		buf.WriteString("Content-Length: ")
		buf.WriteString(strconv.Itoa(len(req.Body)))
		buf.WriteString("\r\n")
	} else if strings.EqualFold(req.Method, http.MethodPost) || strings.EqualFold(req.Method, http.MethodPut) {
		buf.WriteString("Content-Length: 0\r\n")
	}

	buf.WriteString("\r\n")
	if len(req.Body) > 0 {
		buf.Write(req.Body)
	}
	return buf.Bytes()
}

func parseRawHTTPResponse(reader *bufio.Reader, requestMethod string) (UpstreamResponse, error) {
	var headerLines []string
	for {
		line, err := readHeaderLineLenient(reader)
		if err != nil {
			// If EOF occurred right after reading headers without a final empty line, treat header end as reached.
			if len(headerLines) > 0 && isCleanOrAbruptEOF(err) {
				break
			}
			return UpstreamResponse{}, err
		}
		if line == "" {
			if len(headerLines) == 0 {
				// Skip leading blank lines if malformed server sent one.
				continue
			}
			break
		}
		headerLines = append(headerLines, line)
	}

	if len(headerLines) == 0 {
		return UpstreamResponse{}, io.EOF
	}

	statusCode, statusText := parseStatusLineLenient(headerLines[0])
	headers := make(http.Header)
	for _, line := range headerLines[1:] {
		colonIdx := strings.IndexByte(line, ':')
		if colonIdx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:colonIdx])
		val := strings.TrimSpace(line[colonIdx+1:])
		if key != "" {
			headers.Add(key, val)
		}
	}

	noBody := (statusCode >= 100 && statusCode <= 199) ||
		statusCode == http.StatusNoContent ||
		statusCode == http.StatusNotModified ||
		strings.EqualFold(requestMethod, http.MethodHead)

	var rawBody []byte
	var err error
	if !noBody {
		te := strings.ToLower(headers.Get("Transfer-Encoding"))
		clStr := strings.TrimSpace(headers.Get("Content-Length"))
		switch {
		case strings.Contains(te, "chunked"):
			rawBody, err = readChunkedBodyLenient(reader)
		case clStr != "":
			if cl, parseErr := strconv.ParseInt(clStr, 10, 64); parseErr == nil {
				if cl <= 0 {
					rawBody = nil
				} else {
					rawBody, err = readExactLenient(reader, cl)
				}
			} else {
				rawBody, err = readUntilEOFLenient(reader)
			}
		default:
			rawBody, err = readUntilEOFLenient(reader)
		}
		if err != nil {
			return UpstreamResponse{}, err
		}
	}

	body, err := decompressIfNeeded(rawBody, strings.ToLower(strings.TrimSpace(headers.Get("Content-Encoding"))))
	if err != nil {
		return UpstreamResponse{}, err
	}
	return UpstreamResponse{
		StatusCode: statusCode,
		StatusText: statusText,
		Headers:    headers,
		Body:       body,
	}, nil
}

func readHeaderLineLenient(r *bufio.Reader) (string, error) {
	var line []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			if len(line) > 0 && isCleanOrAbruptEOF(err) {
				return strings.TrimRight(string(line), "\r"), nil
			}
			return "", err
		}
		if b == '\n' {
			return strings.TrimRight(string(line), "\r"), nil
		}
		line = append(line, b)
		if len(line) > 64*1024 {
			return "", errors.New("upstream HTTP header line exceeded 64KB")
		}
	}
}

func parseStatusLineLenient(line string) (int, string) {
	parts := strings.SplitN(strings.TrimSpace(line), " ", 3)
	if len(parts) < 2 {
		return 200, "OK"
	}
	code, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || code < 100 || code > 599 {
		return 200, "OK"
	}
	text := "OK"
	if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
		text = strings.TrimSpace(parts[2])
	}
	return code, text
}

func readExactLenient(r *bufio.Reader, n int64) ([]byte, error) {
	if n <= 0 {
		return nil, nil
	}
	limit := min(n, int64(maxResponseBodyBytes)+1)
	bufSize := int(min(limit, 32*1024))
	return readBoundedStream(io.LimitReader(r, limit), maxResponseBodyBytes, bufSize, true)
}

func readUntilEOFLenient(r *bufio.Reader) ([]byte, error) {
	return readBoundedStream(r, maxResponseBodyBytes, 8192, true)
}

func readBoundedStream(r io.Reader, maxBytes int64, bufSize int, tolerateAbruptEOF bool) ([]byte, error) {
	if bufSize <= 0 {
		bufSize = 8192
	}
	var out bytes.Buffer
	chunk := make([]byte, bufSize)
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			if int64(out.Len())+int64(n) > maxBytes {
				return nil, errResponseTooLarge
			}
			out.Write(chunk[:n])
		}
		if err != nil {
			if err == io.EOF || (tolerateAbruptEOF && isCleanOrAbruptEOF(err)) {
				return out.Bytes(), nil
			}
			return nil, err
		}
	}
}

func readChunkedBodyLenient(r *bufio.Reader) ([]byte, error) {
	var out bytes.Buffer
	for {
		sizeLine, err := readHeaderLineLenient(r)
		if err != nil {
			if isCleanOrAbruptEOF(err) {
				return out.Bytes(), nil
			}
			return nil, err
		}
		sizeLine = strings.TrimSpace(sizeLine)
		if sizeLine == "" {
			continue
		}
		hexPart := strings.TrimSpace(strings.SplitN(sizeLine, ";", 2)[0])
		chunkSize, parseErr := strconv.ParseInt(hexPart, 16, 64)
		if parseErr != nil || chunkSize < 0 {
			return out.Bytes(), nil
		}
		if chunkSize == 0 {
			for {
				trailer, tErr := readHeaderLineLenient(r)
				if tErr != nil || trailer == "" {
					break
				}
			}
			return out.Bytes(), nil
		}

		remaining := int64(maxResponseBodyBytes) - int64(out.Len())
		chunk, readErr := readExactLenient(r, min(chunkSize, remaining+1))
		if int64(out.Len())+int64(len(chunk)) > maxResponseBodyBytes {
			return nil, errResponseTooLarge
		}
		out.Write(chunk)
		if readErr != nil {
			return nil, readErr
		}
		_, _ = readHeaderLineLenient(r)
	}
}

func decompressIfNeeded(body []byte, encoding string) ([]byte, error) {
	if len(body) == 0 || encoding == "" {
		return body, nil
	}
	switch {
	case strings.Contains(encoding, "gzip"):
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return body, nil
		}
		defer gr.Close()
		decompressed, err := readBoundedStream(io.LimitReader(gr, maxResponseBodyBytes+1), maxResponseBodyBytes, 8192, false)
		if errors.Is(err, errResponseTooLarge) {
			return nil, err
		}
		if err != nil {
			return body, nil
		}
		return decompressed, nil
	case strings.Contains(encoding, "deflate"):
		if zr, err := zlib.NewReader(bytes.NewReader(body)); err == nil {
			defer zr.Close()
			decompressed, rErr := readBoundedStream(io.LimitReader(zr, maxResponseBodyBytes+1), maxResponseBodyBytes, 8192, false)
			if errors.Is(rErr, errResponseTooLarge) {
				return nil, rErr
			}
			if rErr == nil {
				return decompressed, nil
			}
		}
		fr := flate.NewReader(bytes.NewReader(body))
		defer fr.Close()
		decompressed, fErr := readBoundedStream(io.LimitReader(fr, maxResponseBodyBytes+1), maxResponseBodyBytes, 8192, false)
		if errors.Is(fErr, errResponseTooLarge) {
			return nil, fErr
		}
		if fErr == nil {
			return decompressed, nil
		}
		return body, nil
	default:
		return body, nil
	}
}

// Legacy embedded servers frequently close TCP right after sending HTTP bodies without sending TLS close_notify.
func isCleanOrAbruptEOF(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unexpected eof") || strings.Contains(msg, "use of closed network connection")
}
