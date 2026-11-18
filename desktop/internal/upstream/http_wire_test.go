package upstream

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
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

func TestBoundedReadExactLenientHugeContentLengthShortBody(t *testing.T) {
	raw := "HTTP/1.1 200 OK\r\nContent-Length: 500000000\r\n\r\nok"
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	resp, err := parseRawHTTPResponse(bufio.NewReader(strings.NewReader(raw)), http.MethodGet)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("parseRawHTTPResponse failed: %v", err)
	}
	if string(resp.Body) != "ok" {
		t.Fatalf("Body = %q, want ok", string(resp.Body))
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4*1024*1024 {
		t.Fatalf("allocated %d bytes for a 2-byte response with huge Content-Length", allocated)
	}
}

type repeatByteReader struct {
	remaining int64
}

func (r *repeatByteReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	for i := 0; i < n; i++ {
		p[i] = 'a'
	}
	r.remaining -= int64(n)
	return n, nil
}

func TestBoundedBodyAndDecompressionLimits(t *testing.T) {
	t.Run("rejects Content-Length stream exceeding maxResponseBodyBytes", func(t *testing.T) {
		hdr := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", maxResponseBodyBytes+1024)
		stream := io.MultiReader(strings.NewReader(hdr), &repeatByteReader{remaining: maxResponseBodyBytes + 1024})
		_, err := parseRawHTTPResponse(bufio.NewReader(stream), http.MethodGet)
		if !errors.Is(err, errResponseTooLarge) {
			t.Fatalf("err = %v, want errResponseTooLarge", err)
		}
	})

	t.Run("rejects EOF stream exceeding maxResponseBodyBytes", func(t *testing.T) {
		hdr := "HTTP/1.0 200 OK\r\n\r\n"
		stream := io.MultiReader(strings.NewReader(hdr), &repeatByteReader{remaining: maxResponseBodyBytes + 1024})
		_, err := parseRawHTTPResponse(bufio.NewReader(stream), http.MethodGet)
		if !errors.Is(err, errResponseTooLarge) {
			t.Fatalf("err = %v, want errResponseTooLarge", err)
		}
	})

	t.Run("rejects chunked stream exceeding maxResponseBodyBytes", func(t *testing.T) {
		chunkHdr := fmt.Sprintf("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n", maxResponseBodyBytes+1024)
		stream := io.MultiReader(strings.NewReader(chunkHdr), &repeatByteReader{remaining: maxResponseBodyBytes + 1024}, strings.NewReader("\r\n0\r\n\r\n"))
		_, err := parseRawHTTPResponse(bufio.NewReader(stream), http.MethodGet)
		if !errors.Is(err, errResponseTooLarge) {
			t.Fatalf("err = %v, want errResponseTooLarge", err)
		}
	})

	t.Run("rejects gzip decompression bomb exceeding maxResponseBodyBytes", func(t *testing.T) {
		var gz bytes.Buffer
		w := gzip.NewWriter(&gz)
		zeros := make([]byte, 1024*1024)
		for written := 0; written <= maxResponseBodyBytes; written += len(zeros) {
			if _, err := w.Write(zeros); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}

		hdr := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Encoding: gzip\r\nContent-Length: %d\r\n\r\n", gz.Len())
		stream := io.MultiReader(strings.NewReader(hdr), bytes.NewReader(gz.Bytes()))
		_, err := parseRawHTTPResponse(bufio.NewReader(stream), http.MethodGet)
		if !errors.Is(err, errResponseTooLarge) {
			t.Fatalf("err = %v, want errResponseTooLarge", err)
		}
	})
}
