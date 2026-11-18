package main

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")

	shellExecute     = shell32.NewProc("ShellExecuteW")
	messageBox       = user32.NewProc("MessageBoxW")
	getConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	attachConsole    = kernel32.NewProc("AttachConsole")
	getStdHandle     = kernel32.NewProc("GetStdHandle")
	createMutex      = kernel32.NewProc("CreateMutexW")
	waitForSingle    = kernel32.NewProc("WaitForSingleObject")
	releaseMutex     = kernel32.NewProc("ReleaseMutex")
	coInitialize     = ole32.NewProc("CoInitializeEx")
	coUninitialize   = ole32.NewProc("CoUninitialize")
)

func openBrowser(url string) error {
	address, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const apartmentThreaded = 0x2
	const disableOLE1DDE = 0x4
	if result, _, _ := coInitialize.Call(0, apartmentThreaded|disableOLE1DDE); int32(result) >= 0 {
		defer coUninitialize.Call()
	}
	const showNormal = 1
	result, _, _ := shellExecute.Call(0, 0, uintptr(unsafe.Pointer(address)), 0, 0, showNormal)
	if result <= 32 {
		return fmt.Errorf("Windows could not open %s (error %d). Check your default browser in Settings", url, result)
	}
	return nil
}

func shouldShowErrorDialog(noBrowser, consoleAttached bool) bool {
	return !noBrowser && !consoleAttached
}

func attachParentConsole() bool {
	const attachParentProcess = ^uintptr(0)
	if console, _, _ := getConsoleWindow.Call(); console != 0 {
		return true
	}
	ok, _, _ := attachConsole.Call(attachParentProcess)
	return ok != 0
}

func writeConsoleLine(stream *os.File, stdHandleID uintptr, line string) bool {
	consoleAttached := attachParentConsole()
	if _, writeErr := fmt.Fprintln(stream, line); writeErr == nil {
		return consoleAttached
	}
	if !consoleAttached {
		return false
	}
	if handle, _, _ := getStdHandle.Call(stdHandleID); handle != 0 && handle != ^uintptr(0) {
		if _, err := syscall.Write(syscall.Handle(handle), []byte(line+"\r\n")); err == nil {
			return true
		}
	}
	if con, openErr := os.OpenFile("CONOUT$", os.O_WRONLY, 0); openErr == nil {
		_, _ = fmt.Fprintln(con, line)
		_ = con.Close()
	}
	return true
}

func writeStdoutLine(line string) {
	const stdOutputHandle = ^uintptr(10) // (DWORD)-11 = STD_OUTPUT_HANDLE
	writeConsoleLine(os.Stdout, stdOutputHandle, line)
}

func reportLaunchError(err error, noBrowser bool) {
	const stdErrorHandle = ^uintptr(11) // (DWORD)-12 = STD_ERROR_HANDLE

	consoleAttached := writeConsoleLine(os.Stderr, stdErrorHandle, err.Error())
	if !shouldShowErrorDialog(noBrowser, consoleAttached) {
		return
	}
	title, _ := syscall.UTF16PtrFromString("Router Page Opener")
	message, _ := syscall.UTF16PtrFromString(err.Error())
	const iconError = 0x10
	const setForeground = 0x10000
	messageBox.Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), iconError|setForeground)
}

func lockStartup(port int) (func(), error) {
	// Windows assigns ownership of a mutex to the calling OS thread.
	runtime.LockOSThread()
	name, _ := syscall.UTF16PtrFromString(fmt.Sprintf(`Local\LegacyRouterOpener-%d`, port))
	handle, _, err := createMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("coordinate startup: %w", err)
	}
	const waitAbandoned = 0x80
	result, _, _ := waitForSingle.Call(handle, 10000)
	if result != 0 && result != waitAbandoned {
		_ = syscall.CloseHandle(syscall.Handle(handle))
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("could not wait for another launch on port %d (Windows status %d)", port, result)
	}
	return func() {
		releaseMutex.Call(handle)
		_ = syscall.CloseHandle(syscall.Handle(handle))
		runtime.UnlockOSThread()
	}, nil
}
