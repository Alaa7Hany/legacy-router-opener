//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func openBrowser(url string) error {
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}
	return exec.Command(command, url).Run()
}

func shouldShowErrorDialog(noBrowser, consoleAttached bool) bool {
	return false
}

func reportLaunchError(err error, _ bool) {
	fmt.Fprintln(os.Stderr, err)
}

func lockStartup(int) (func(), error) {
	return func() {}, nil
}
