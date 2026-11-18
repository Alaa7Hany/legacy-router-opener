//go:debug tlssha1=1
//go:debug rsa1024min=0
//go:debug x509negativeserial=1

package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

type launchOptions struct {
	port                  int
	target, preset        string
	noBrowser, openRouter bool
}

func main() {
	options := parseLaunchOptions()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, options); err != nil {
		reportLaunchError(err, options.noBrowser)
		os.Exit(1)
	}
}

func parseLaunchOptions() launchOptions {
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

	return launchOptions{
		port: *portFlag, target: initialTarget, preset: *presetFlag,
		noBrowser: *noBrowserFlag, openRouter: *openRouterFlag,
	}
}
