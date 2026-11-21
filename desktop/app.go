package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alaa/legacyrouteropener/internal/gateway"
	"github.com/alaa/legacyrouteropener/internal/proxy"
	"github.com/alaa/legacyrouteropener/internal/upstream"
)

type desktopApp struct {
	proxy, control *http.Server
	process        *bridgeProcess
}

func run(ctx context.Context, options launchOptions) error {
	app, launchURL, err := startApp(options)
	if err != nil {
		return fmt.Errorf("Could not start Router Page Opener: %w", err)
	}
	if app != nil {
		defer app.shutdown()
	}
	if !options.noBrowser {
		if err := openBrowser(launchURL); err != nil {
			return fmt.Errorf("Could not open the default browser: %w", err)
		}
	}
	if app == nil {
		return nil
	}

	select {
	case <-ctx.Done():
		return nil
	case stopErr := <-app.process.stopSignal():
		return stopError(stopErr)
	}
}

// stopError maps the stop signal onto the launch result: a clean quit carries
// no error, while a failed server is reported as an unexpected stop.
func stopError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("Router Page Opener stopped unexpectedly: %w", err)
}

func startApp(options launchOptions) (*desktopApp, string, error) {
	initialTarget := strings.TrimSpace(options.target)
	if initialTarget != "" {
		if _, err := upstream.ValidateTarget(initialTarget); err != nil {
			return nil, "", fmt.Errorf("invalid target: %w", err)
		}
	}

	requestedPort := validProxyPort(options.port)
	var startupTimeout time.Duration
	if !options.noBrowser && !options.openRouter {
		startupTimeout = openerStartupTimeout
	}
	process, err := newBridgeProcess(requestedPort, startupTimeout)
	if err != nil {
		return nil, "", err
	}
	launchURL := process.launchURL(options.openRouter)
	if process.reusedBridge() {
		fmt.Printf("[INFO] Reusing Router Page Opener on port %d.\n", process.proxyPort())
		return nil, launchURL, nil
	}

	detectedGateway := gateway.DetectDefaultGateway()
	target := initialTarget
	if target == "" {
		target = detectedGateway
	}
	app := &desktopApp{process: process}
	quit := newQuitHandler(process.controlPort(), process)
	bridge := proxy.NewServer(proxy.Options{
		ProxyPort:       process.proxyPort(),
		ControlPort:     process.controlPort(),
		RequestedPort:   process.requestedPort(),
		InitialTarget:   target,
		InitialPresetID: options.preset,
		DetectedGateway: detectedGateway,
		OnStdoutLog:     func(line string) { fmt.Println(line) },
		QuitURL:         quitPath,
		Stopping:        process.streamStop(),
	})
	if initialTarget != "" {
		if err := bridge.CheckTargetAllowed(initialTarget); err != nil {
			process.close()
			return nil, "", err
		}
	}

	controlHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isOpenerStream(w, r, process.controlPort()) {
			endTab := process.registerOpener()
			defer endTab()
		}
		if r.URL.Path == quitPath || strings.HasPrefix(r.URL.Path, quitPath+"/") {
			quit.ServeHTTP(w, r)
			return
		}
		bridge.ControlHandler().ServeHTTP(w, r)
	})

	app.proxy = &http.Server{
		Handler:           process.proxyHandler(bridge.ProxyHandler()),
		ReadHeaderTimeout: 30 * time.Second,
	}
	app.control = &http.Server{
		Handler:           controlHandler,
		ReadHeaderTimeout: 30 * time.Second,
	}
	proxyBindings, controlBindings := process.listeners()
	for _, listener := range proxyBindings {
		go app.serve(app.proxy, listener)
	}
	for _, listener := range controlBindings {
		go app.serve(app.control, listener)
	}

	snapshot := bridge.Snapshot()
	fmt.Printf("Router Page Opener\nGateway: %s\nTarget: %s (%s)\nOpener Page: %s\nRouter Page: http://%s:%d/\nPress Ctrl+C or use Quit app to stop.\n",
		detectedGateway, snapshot.ResolvedTarget, upstream.FindPreset(snapshot.Preset).Name, process.openerURL(), openerHost, process.proxyPort())
	return app, launchURL, nil
}

// isOpenerStream reports whether r is the same-origin Opener event stream the
// control origin will actually serve. The read guards live inside the control
// plane, so the launch wrapper mirrors them here: a rejected stream must never
// register a tab and schedule the four-second stop.
func isOpenerStream(w http.ResponseWriter, r *http.Request, controlPort int) bool {
	if r.URL.Path != "/_bridge/events" || r.Method != http.MethodGet ||
		r.URL.Query().Get("opener") != "1" ||
		r.Header.Get("Sec-Fetch-Site") != "same-origin" ||
		!proxy.IsAllowedLoopbackHost(r.Host, controlPort) {
		return false
	}
	_, ok := w.(http.Flusher)
	return ok
}

func (app *desktopApp) serve(server *http.Server, listener net.Listener) {
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		app.process.stop(err)
	}
}

func (app *desktopApp) shutdown() {
	// Ending the streams first lets Shutdown finish waiting on active requests.
	app.process.endStreams()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Shutdown closes every listener handed to Serve, so a normal stop makes
	// Serve return http.ErrServerClosed rather than a spurious serve failure.
	var group sync.WaitGroup
	for _, server := range []*http.Server{app.proxy, app.control} {
		group.Go(func() {
			if err := server.Shutdown(ctx); err != nil {
				_ = server.Close()
			}
		})
	}
	group.Wait()
	// Clean up only the bindings the servers did not close themselves.
	app.process.closeBoundListeners()
}

// runResult is the launch result run returns for the next stop signal. run
// binds its own listeners, so a test cannot close one through run; it uses
// this to assert the exact error string a serve failure produces.
func (app *desktopApp) runResult() error {
	return stopError(<-app.process.stopSignal())
}
