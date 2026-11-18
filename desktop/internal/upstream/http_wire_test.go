package upstream

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestLenientRawHTTPParser(t *testing.T) {
	t.Run("HTTP/1.0 without status text or Content-Length", func(t *testing.T) {
		raw := "HTTP/1.0 200\r\nSet-Cookie: A=1\r\nSet-Cookie: B=2\r\n\r\n<html>ok</html>"
		resp, err := parseRawHTTPResponse(bufio.NewReader(strings.NewReader(raw)), http.MethodGet)
		if err != nil {
			t.Fatalf("parseRawHTTPResponse failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
		}
		if len(resp.Headers.Values("Set-Cookie")) != 2 {
			t.Fatalf("expected 2 Set-Cookie headers, got %v", resp.Headers.Values("Set-Cookie"))
		}
		if string(resp.Body) != "<html>ok</html>" {
			t.Fatalf("Body = %q", string(resp.Body))
		}
	})

	t.Run("chunked transfer encoding with extensions", func(t *testing.T) {
		raw := "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n5;ext=1\r\nhello\r\n6\r\n world\r\n0\r\n\r\n"
		resp, err := parseRawHTTPResponse(bufio.NewReader(strings.NewReader(raw)), http.MethodGet)
		if err != nil {
			t.Fatalf("parseRawHTTPResponse failed: %v", err)
		}
		if string(resp.Body) != "hello world" {
			t.Fatalf("chunked Body = %q, want %q", string(resp.Body), "hello world")
		}
	})

	t.Run("gzip encoded response body", func(t *testing.T) {
		var gz bytes.Buffer
		w := gzip.NewWriter(&gz)
		_, _ = w.Write([]byte("compressed-router-html"))
		_ = w.Close()

		raw := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Encoding: gzip\r\nContent-Length: %d\r\n\r\n", gz.Len())
		full := append([]byte(raw), gz.Bytes()...)
		resp, err := parseRawHTTPResponse(bufio.NewReader(bytes.NewReader(full)), http.MethodGet)
		if err != nil {
			t.Fatalf("parseRawHTTPResponse failed: %v", err)
		}
		if string(resp.Body) != "compressed-router-html" {
			t.Fatalf("gzip Body = %q", string(resp.Body))
		}
	})
}
