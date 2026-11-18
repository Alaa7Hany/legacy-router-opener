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
	stopping       chan struct{}
	quitRequested  chan struct{}
	serveErrors    chan error
	tabs           *tabLifetime
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
	unlock, err := lockStartup(requestedPort)
	if err != nil {
		return nil, "", err
	}
	defer unlock()

	listeners, port, existing, existingControlPort, err := listenWithFallback(requestedPort)
	if err != nil {
		return nil, "", err
	}
	if existing {
		openerPort := existingControlPort
		if openerPort <= 0 {
			openerPort = port
		}
		openerURL := fmt.Sprintf("http://localhost:%d/_bridge", openerPort)
		launchURL := openerURL
		if options.openRouter {
			launchURL = fmt.Sprintf("http://localhost:%d/", port)
		}
		fmt.Printf("[INFO] Reusing Router Page Opener on port %d.\n", port)
		return nil, launchURL, nil
	}

	controlListeners, controlPort, err := listenControlWithFallback(port)
	if err != nil {
		closeListeners(listeners)
		return nil, "", err
	}
	openerURL := fmt.Sprintf("http://localhost:%d/_bridge", controlPort)
	launchURL := openerURL
	if options.openRouter {
		launchURL = fmt.Sprintf("http://localhost:%d/", port)
	}

	detectedGateway := gateway.DetectDefaultGateway()
	target := initialTarget
	if target == "" {
		target = detectedGateway
	}
	app := &desktopApp{
		stopping:      make(chan struct{}),
		quitRequested: make(chan struct{}),
		serveErrors:   make(chan error, len(listeners)+len(controlListeners)),
	}
	requestStop := sync.OnceFunc(func() {
		close(app.quitRequested)
	})
	var startupTimeout time.Duration
	if !options.noBrowser && !options.openRouter {
		startupTimeout = openerStartupTimeout
	}
	app.tabs = newTabLifetime(startupTimeout, requestStop)
	quit := newQuitHandler(controlPort, requestStop)
	quit.watchTab = app.tabs.connect
	quit.stopping = app.stopping
	bridge := proxy.NewServer(proxy.Options{
		ProxyPort:       port,
		ControlPort:     controlPort,
		RequestedPort:   requestedPort,
		InitialTarget:   target,
		InitialPresetID: options.preset,
		DetectedGateway: detectedGateway,
		OnStdoutLog:     func(line string) { fmt.Println(line) },
		QuitURL:         quitPath,
		Stopping:        app.stopping,
		WatchOpenerTab:  app.tabs.connect,
	})
	if initialTarget != "" {
		if err := bridge.CheckTargetAllowed(initialTarget); err != nil {
			app.tabs.close()
			closeListeners(listeners)
			closeListeners(controlListeners)
			return nil, "", err
		}
	}

	controlHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == quitPath || strings.HasPrefix(r.URL.Path, quitPath+"/") {
			quit.ServeHTTP(w, r)
			return
		}
		bridge.ControlHandler().ServeHTTP(w, r)
	})

	app.proxy = &http.Server{
		Handler:           withInstancePort(bridge.ProxyHandler(), requestedPort),
		ReadHeaderTimeout: 30 * time.Second,
	}
	app.control = &http.Server{
		Handler:           controlHandler,
		ReadHeaderTimeout: 30 * time.Second,
	}
	for _, listener := range listeners {
		go app.serve(app.proxy, listener)
	}
	for _, listener := range controlListeners {
		go app.serve(app.control, listener)
	}

	snapshot := bridge.Snapshot()
	fmt.Printf("Router Page Opener\nGateway: %s\nTarget: %s (%s)\nOpener Page: %s\nRouter Page: http://localhost:%d/\nPress Ctrl+C or use Quit app to stop.\n",
		detectedGateway, snapshot.ResolvedTarget, upstream.FindPreset(snapshot.Preset).Name, openerURL, port)
	return app, launchURL, nil
}

func (app *desktopApp) serve(server *http.Server, listener net.Listener) {
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		app.serveErrors <- err
	}
}

func (app *desktopApp) shutdown() {
	app.tabs.close()
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
