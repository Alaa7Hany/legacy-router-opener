package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestComposedBridgeLifecycle drives the whole Bridge lifetime through the
// launch interface: one Bridge starts, a second launch reuses it, an Opener
// stream attaches and closes, and the first Bridge stops cleanly after the
// reconnect grace. Ports come from real ephemeral loopback binds (the launch
// helpers own and clean up their listeners); no identity is fabricated.
// Serve-failure reporting is already asserted by TestServeFailureReportsError.
func TestComposedBridgeLifecycle(t *testing.T) {
	port := freeLoopbackPort(t)
	firstDone, client, controlPort := launchBridge(t, port)

	// A second launch must discover the running Bridge through its ping headers
	// and return at once instead of binding another copy.
	secondCtx, cancelSecond := context.WithCancel(t.Context())
	defer cancelSecond()
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- run(secondCtx, launchOptions{
			port: port, target: unreachableTarget, noBrowser: true,
		})
	}()
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second launch failed to reuse the Bridge: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second launch did not reuse the running Bridge")
	}

	// The original Bridge is still serving with an unchanged advertised identity.
	ping, err := client.Get(fmt.Sprintf("http://localhost:%d%s", port, pingPath))
	if err != nil {
		t.Fatalf("ping running Bridge: %v", err)
	}
	_ = ping.Body.Close()
	if ping.StatusCode != http.StatusOK ||
		ping.Header.Get("X-Legacy-Router-Proxy") != "1" ||
		ping.Header.Get(requestedPortField) != strconv.Itoa(port) ||
		ping.Header.Get(controlPortField) != controlPort {
		t.Fatalf("reused Bridge identity changed: status=%d headers=%v", ping.StatusCode, ping.Header)
	}

	// Attach the Opener stream on the control origin with the required
	// same-origin header, confirm it starts, then close it to drop the last tab.
	stream, err := client.Do(mustRequest(t, http.MethodGet,
		"http://localhost:"+controlPort+"/_bridge/events?opener=1", "same-origin"))
	if err != nil {
		t.Fatalf("open Opener stream: %v", err)
	}
	if stream.StatusCode != http.StatusOK || stream.Header.Get("Content-Type") != "text/event-stream" {
		_ = stream.Body.Close()
		t.Fatalf("Opener stream = (%d, %q)", stream.StatusCode, stream.Header.Get("Content-Type"))
	}
	line, err := bufio.NewReader(stream.Body).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "data: ") {
		_ = stream.Body.Close()
		t.Fatalf("Opener stream did not start: %q (%v)", line, err)
	}
	_ = stream.Body.Close()

	// Last tab gone: the first Bridge stops after the grace period, with no error.
	requireStop(t, firstDone, tabReconnectGrace+3*time.Second, false)
}
