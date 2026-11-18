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

func reportLaunchError(err error) {
	fmt.Fprintln(os.Stderr, err)
}

func lockStartup(int) (func(), error) {
	return func() {}, nil
}
