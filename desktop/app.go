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
	stopping       chan struct{}
	quitRequested  chan struct{}
	serveErrors    chan error
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
	case <-app.quitRequested:
	case err := <-app.serveErrors:
		return fmt.Errorf("Router Page Opener stopped unexpectedly: %w", err)
	}
	return nil
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
	quitRequested := make(chan struct{})
	requestStop := sync.OnceFunc(func() {
		close(quitRequested)
	})

	process, err := newBridgeProcess(requestedPort, startupTimeout, requestStop)
	if err != nil {
		return nil, "", err
	}
	identity := process.identity
	launchURL := process.launchURL(options.openRouter)
	if process.reused {
		fmt.Printf("[INFO] Reusing Router Page Opener on port %d.\n", identity.proxyPort)
		return nil, launchURL, nil
	}

	detectedGateway := gateway.DetectDefaultGateway()
	target := initialTarget
	if target == "" {
		target = detectedGateway
	}
	app := &desktopApp{
		process:       process,
		stopping:      make(chan struct{}),
		quitRequested: quitRequested,
		serveErrors:   make(chan error, len(process.proxyListeners)+len(process.controlListeners)),
	}
	quit := newQuitHandler(identity.controlPort, requestStop)
	quit.watchTab = process.registerOpener
	quit.stopping = app.stopping
	bridge := proxy.NewServer(proxy.Options{
		ProxyPort:       identity.proxyPort,
		ControlPort:     identity.controlPort,
		RequestedPort:   identity.requestedPort,
		InitialTarget:   target,
		InitialPresetID: options.preset,
		DetectedGateway: detectedGateway,
		OnStdoutLog:     func(line string) { fmt.Println(line) },
		QuitURL:         quitPath,
		Stopping:        app.stopping,
	})
	if initialTarget != "" {
		if err := bridge.CheckTargetAllowed(initialTarget); err != nil {
			process.close()
			return nil, "", err
		}
	}

	controlHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_bridge/events" && r.URL.Query().Get("opener") == "1" &&
			r.Header.Get("Sec-Fetch-Site") == "same-origin" {
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
	for _, listener := range process.proxyListeners {
		go app.serve(app.proxy, listener)
	}
	for _, listener := range process.controlListeners {
		go app.serve(app.control, listener)
	}

	snapshot := bridge.Snapshot()
	fmt.Printf("Router Page Opener\nGateway: %s\nTarget: %s (%s)\nOpener Page: %s\nRouter Page: http://localhost:%d/\nPress Ctrl+C or use Quit app to stop.\n",
		detectedGateway, snapshot.ResolvedTarget, upstream.FindPreset(snapshot.Preset).Name, process.openerURL(), identity.proxyPort)
	return app, launchURL, nil
}

func (app *desktopApp) serve(server *http.Server, listener net.Listener) {
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		app.serveErrors <- err
	}
}

func (app *desktopApp) shutdown() {
	app.process.close()
	close(app.stopping)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var group sync.WaitGroup
	for _, server := range []*http.Server{app.proxy, app.control} {
		group.Go(func() {
			if err := server.Shutdown(ctx); err != nil {
				_ = server.Close()
			}
		})
	}
	group.Wait()
}
