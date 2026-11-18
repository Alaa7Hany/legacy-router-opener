//go:debug tlssha1=1
//go:debug rsa1024min=0
//go:debug x509negativeserial=1

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

var version = "dev"

type launchOptions struct {
	port                               int
	target, preset                     string
	noBrowser, openRouter, showVersion bool
}

func formatVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		v = "dev"
	}
	return "legacy-router-opener " + v
}

func main() {
	var flagOut bytes.Buffer
	options, err := parseLaunchOptionsArgs(os.Args[1:], os.Getenv, &flagOut)
	if err != nil {
		msg := strings.TrimRight(flagOut.String(), "\r\n")
		if msg == "" {
			msg = err.Error()
		}
		if errors.Is(err, flag.ErrHelp) {
			writeStdoutLine(msg)
			return
		}
		reportLaunchError(errors.New(msg), options.noBrowser)
		os.Exit(2)
	}
	if options.showVersion {
		writeStdoutLine(formatVersion(version))
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, options); err != nil {
		reportLaunchError(err, options.noBrowser)
		os.Exit(1)
	}
}

func parseLaunchOptionsArgs(args []string, getenv func(string) string, output io.Writer) (launchOptions, error) {
	defaultPort := 8080
	if getenv != nil {
		if envPort := strings.TrimSpace(getenv("PROXY_PORT")); envPort != "" {
			if p, err := strconv.Atoi(envPort); err == nil && p >= 1 && p <= 65535 {
				defaultPort = p
			}
		}
	}

	var defaultTarget string
	if getenv != nil {
		defaultTarget = strings.TrimSpace(getenv("ROUTER_IP"))
	}

	fs := flag.NewFlagSet("legacy-router-opener", flag.ContinueOnError)
	if output != nil {
		fs.SetOutput(output)
	}
	portFlag := fs.Int("port", defaultPort, "Local proxy listening port (falls back to next free port if busy)")
	targetFlag := fs.String("target", defaultTarget, "Target router or device IP (optional :port or http://)")
	presetFlag := fs.String("preset", "auto", "Hardware compatibility preset (auto, huawei-zte, idrac, ilo, apc, cisco-hp, printer-nas)")
	noBrowserFlag := fs.Bool("no-browser", false, "Do not auto-open the web browser on startup")
	openRouterFlag := fs.Bool("open-router", false, "Open the proxied device root (/) instead of the /_bridge Control Launchpad")
	versionFlag := fs.Bool("version", false, "Print version information and exit")
	if err := fs.Parse(args); err != nil {
		return launchOptions{noBrowser: *noBrowserFlag}, err
	}

	initialTarget := strings.TrimSpace(*targetFlag)
	if initialTarget == "" && fs.NArg() > 0 {
		initialTarget = strings.TrimSpace(fs.Arg(0))
	}

	return launchOptions{
		port: *portFlag, target: initialTarget, preset: *presetFlag,
		noBrowser: *noBrowserFlag, openRouter: *openRouterFlag, showVersion: *versionFlag,
	}, nil
}
