//go:debug tlssha1=1
//go:debug rsa1024min=0
//go:debug x509negativeserial=1

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alaa/legacyrouteropener/internal/proxy"
)

const maxPortAttempts = 10

func main() {
	defaultPort := 8080
	if envPort := strings.TrimSpace(os.Getenv("PROXY_PORT")); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p >= 1 && p <= 65535 {
			defaultPort = p
		}
	}

	portFlag := flag.Int("port", defaultPort, "Local proxy listening port (falls back to next free port if busy)")
	targetFlag := flag.String("target", strings.TrimSpace(os.Getenv("ROUTER_IP")), "Target router or device IP (optional :port or http://)")
	presetFlag := flag.String("preset", "auto", "Hardware compatibility preset (auto, huawei-zte, idrac, ilo, apc, cisco-hp, printer-nas)")
	noBrowserFlag := flag.Bool("no-browser", false, "Do not auto-open the web browser on startup")
	openRouterFlag := flag.Bool("open-router", false, "Open the proxied device root (/) instead of the /_bridge Control Launchpad")
	flag.Parse()

	initialTarget := strings.TrimSpace(*targetFlag)
	if initialTarget == "" && flag.NArg() > 0 {
		initialTarget = strings.TrimSpace(flag.Arg(0))
	}

	detectedGateway := proxy.DetectDefaultGateway()
	if initialTarget == "" {
		initialTarget = detectedGateway
	}

	srv := proxy.NewServer(proxy.Options{
		ProxyPort:       *portFlag,
		InitialTarget:   initialTarget,
		InitialPresetID: *presetFlag,
		DetectedGateway: detectedGateway,
		OnStdoutLog: func(line string) {
			fmt.Println(line)
		},
	})

	listeners, boundPort, alreadyRunning, err := listenWithFallback(*portFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[Proxy ❌ Startup Error] %v\n", err)
		os.Exit(1)
	}

	launchPath := "/_bridge"
	if *openRouterFlag {
		launchPath = "/"
	}
	launchURL := fmt.Sprintf("http://localhost:%d%s", boundPort, launchPath)

	if alreadyRunning {
		fmt.Println("\n======================================================")
		fmt.Printf("ℹ️  Port %d is already serving Legacy Router Opener.\n", boundPort)
		fmt.Printf("👉 Control Launchpad: http://localhost:%d/_bridge\n", boundPort)
		fmt.Printf("👉 Device Dashboard:  http://localhost:%d/\n", boundPort)
		fmt.Println("🔒 Reusing existing instance to avoid router session conflicts.")
		fmt.Println("======================================================")
		if !*noBrowserFlag {
			_ = openBrowser(launchURL)
		}
		return
	}

	srv.SetProxyPort(boundPort)

	httpServer := &http.Server{
		Handler:           srv,
		ReadHeaderTimeout: 30 * time.Second,
	}

	snap := srv.Snapshot()
	fmt.Println("\n======================================================")
	fmt.Println("🚀 Legacy Router & Device Opener (Go Standalone Bridge)")
	fmt.Printf("🔍 Detected Gateway:  %s\n", detectedGateway)
	fmt.Printf("🎯 Active Target:     %s (%s)\n", snap.ResolvedTarget, proxy.FindPreset(snap.Preset).Name)
	fmt.Printf("🎛️  Control Launchpad: http://localhost:%d/_bridge\n", boundPort)
	fmt.Printf("🌐 Device Dashboard:  http://localhost:%d/\n", boundPort)
	fmt.Println("======================================================")
	fmt.Println("Press Ctrl+C to stop.")

	errCh := make(chan error, len(listeners))
	for _, ln := range listeners {
		l := ln
		go func() {
			if serveErr := httpServer.Serve(l); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				errCh <- serveErr
			}
		}()
	}

	if !*noBrowserFlag {
		go func() {
			time.Sleep(150 * time.Millisecond)
			_ = openBrowser(launchURL)
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		fmt.Printf("\n⏹️  Received %s, shutting down...\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = httpServer.Shutdown(ctx)
		cancel()
	case serveErr := <-errCh:
		fmt.Fprintf(os.Stderr, "[Proxy ❌ Server Error] %v\n", serveErr)
		os.Exit(1)
	}
}

func listenWithFallback(startPort int) ([]net.Listener, int, bool, error) {
	if startPort <= 0 || startPort > 65535 {
		startPort = 8080
	}

	for offset := 0; offset < maxPortAttempts; offset++ {
		candidate := startPort + offset
		if candidate > 65535 {
			break
		}

		ln4, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", candidate))
		if err == nil {
			listeners := []net.Listener{ln4}
			// Also bind IPv6 loopback on the same port when available so http://localhost works without delay.
			if ln6, err6 := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", candidate)); err6 == nil {
				listeners = append(listeners, ln6)
			}
			return listeners, candidate, false, nil
		}

		if offset == 0 && proxy.IsExistingProxy(candidate) {
			return nil, candidate, true, nil
		}

		if offset == 0 {
			fmt.Printf("[Proxy ⚠️] Port %d is in use by another app; trying fallback port...\n", candidate)
		} else {
			fmt.Printf("[Proxy ⚠️] Port %d is also in use; trying next port...\n", candidate)
		}
	}

	return nil, 0, false, fmt.Errorf("no available proxy port found in range %d-%d", startPort, startPort+maxPortAttempts-1)
}

func openBrowser(targetURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL)
	case "darwin":
		cmd = exec.Command("open", targetURL)
	default:
		cmd = exec.Command("xdg-open", targetURL)
	}
	return cmd.Start()
}
