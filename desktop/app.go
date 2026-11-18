package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/alaa/legacyrouteropener/internal/gateway"
	"github.com/alaa/legacyrouteropener/internal/proxy"
	"github.com/alaa/legacyrouteropener/internal/upstream"
)

type desktopApp struct {
	bridge, quit  *http.Server
	stopping      chan struct{}
	quitRequested chan struct{}
	serveErrors   chan error
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
	requestedPort := validProxyPort(options.port)
	unlock, err := lockStartup(requestedPort)
	if err != nil {
		return nil, "", err
	}
	defer unlock()

	listeners, port, existing, err := listenWithFallback(requestedPort)
	if err != nil {
		return nil, "", err
	}
	openerURL := fmt.Sprintf("http://localhost:%d/_bridge", port)
	launchURL := openerURL
	if options.openRouter {
		launchURL = fmt.Sprintf("http://localhost:%d/", port)
	}
	if existing {
		fmt.Printf("[INFO] Reusing Router Page Opener on port %d.\n", port)
		return nil, launchURL, nil
	}

	quitListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		for _, listener := range listeners {
			_ = listener.Close()
		}
		return nil, "", fmt.Errorf("listen for browser Quit requests: %w", err)
	}

	detectedGateway := gateway.DetectDefaultGateway()
	target := options.target
	if target == "" {
		target = detectedGateway
	}
	app := &desktopApp{
		stopping:      make(chan struct{}),
		quitRequested: make(chan struct{}),
		serveErrors:   make(chan error, len(listeners)+1),
	}
	quitHost := quitListener.Addr().String()
	quit := newQuitHandler(quitHost, openerURL, sync.OnceFunc(func() {
		close(app.quitRequested)
	}))
	bridge := proxy.NewServer(proxy.Options{
		ProxyPort:       port,
		InitialTarget:   target,
		InitialPresetID: options.preset,
		DetectedGateway: detectedGateway,
		OnStdoutLog:     func(line string) { fmt.Println(line) },
		QuitURL:         "http://" + quitHost + quitPath,
		Stopping:        app.stopping,
	})
	app.bridge = &http.Server{
		Handler:           withInstancePort(bridge, requestedPort),
		ReadHeaderTimeout: 30 * time.Second,
	}
	app.quit = &http.Server{
		Handler:      quit,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	for _, listener := range listeners {
		go app.serve(app.bridge, listener)
	}
	go app.serve(app.quit, quitListener)

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
	close(app.stopping)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var group sync.WaitGroup
	for _, server := range []*http.Server{app.bridge, app.quit} {
		group.Go(func() {
			if err := server.Shutdown(ctx); err != nil {
				_ = server.Close()
			}
		})
	}
	group.Wait()
}
