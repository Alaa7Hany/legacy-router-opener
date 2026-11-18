package main

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

const (
	maxPortAttempts    = 10
	pingPath           = "/_bridge/api/ping"
	requestedPortField = "X-Legacy-Router-Requested-Port"
)

func validProxyPort(port int) int {
	if port < 1 || port > 65535 {
		return 8080
	}
	return port
}

func listenWithFallback(startPort int) ([]net.Listener, int, bool, error) {
	startPort = validProxyPort(startPort)
	lastPort := min(startPort+maxPortAttempts-1, 65535)
	var selected net.Listener
	var boundPort int

	for port := startPort; port <= lastPort; port++ {
		listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			if selected == nil {
				selected, boundPort = listener, port
			} else {
				_ = listener.Close()
			}
			continue
		}

		// A previous launch may have used a fallback port even if the preferred port is free now.
		if isExistingProxy(port, startPort, selected == nil) {
			if selected != nil {
				_ = selected.Close()
			}
			return nil, port, true, nil
		}
	}

	if selected == nil {
		return nil, 0, false, fmt.Errorf("no available proxy port in range %d–%d", startPort, lastPort)
	}
	listeners := []net.Listener{selected}
	if listener, err := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", boundPort)); err == nil {
		listeners = append(listeners, listener)
	}
	return listeners, boundPort, false, nil
}

func withInstancePort(bridge http.Handler, requestedPort int) http.Handler {
	port := strconv.Itoa(requestedPort)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pingPath {
			w.Header().Set(requestedPortField, port)
		}
		bridge.ServeHTTP(w, r)
	})
}

func isExistingProxy(port, requestedPort int, allowLegacy bool) bool {
	client := &http.Client{
		Timeout: 350 * time.Millisecond,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	probe := func(path string) (bool, string) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
		if err != nil {
			return false, ""
		}
		_ = resp.Body.Close()
		return resp.Header.Get("X-Legacy-Router-Proxy") == "1", resp.Header.Get(requestedPortField)
	}
	if found, originalPort := probe(pingPath); found {
		return port == requestedPort || originalPort == strconv.Itoa(requestedPort) ||
			(allowLegacy && originalPort == "")
	}
	if port == requestedPort {
		// Older Node.js launches identify themselves on the router polling endpoint.
		found, _ := probe("/html/ajaxref/updatachallangeajax.asp")
		return found
	}
	return false
}
