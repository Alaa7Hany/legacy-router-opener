package main

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestInteractiveLaunchStopsIfNoOpenerConnects(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stopped := false
		tabs := newTabLifetime(openerStartupTimeout, func() { stopped = true })
		defer tabs.close()

		time.Sleep(openerStartupTimeout)
		synctest.Wait()
		if !stopped {
			t.Fatal("the bridge should stop if the opener never connects")
		}
	})
}

func TestLastTabControlsShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stopped := false
		tabs := newTabLifetime(openerStartupTimeout, func() { stopped = true })
		defer tabs.close()

		endFirst := tabs.connect()
		endSecond := tabs.connect()
		endFirst()
		time.Sleep(openerStartupTimeout)
		synctest.Wait()
		if stopped {
			t.Fatal("another open tab should keep the bridge running")
		}

		endSecond()
		time.Sleep(tabReconnectGrace / 2)
		endReload := tabs.connect()
		time.Sleep(tabReconnectGrace)
		synctest.Wait()
		if stopped {
			t.Fatal("reloading during the grace period should keep the bridge running")
		}

		endReload()
		time.Sleep(tabReconnectGrace)
		synctest.Wait()
		if !stopped {
			t.Fatal("closing the last tab should stop the bridge after the grace period")
		}
	})
}

func TestManualLaunchCanWaitForAnOpener(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stopped := false
		tabs := newTabLifetime(0, func() { stopped = true })
		defer tabs.close()

		time.Sleep(openerStartupTimeout)
		synctest.Wait()
		if stopped {
			t.Fatal("a manual launch should wait for an opener")
		}

		endTab := tabs.connect()
		endTab()
		time.Sleep(tabReconnectGrace)
		synctest.Wait()
		if !stopped {
			t.Fatal("a manual launch should stop after its opener closes")
		}
	})
}

func TestActiveRouterSessionSurvivesOpenerCloseAndResetsIdleTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stopped := false
		tabs := newTabLifetime(openerStartupTimeout, func() { stopped = true })
		defer tabs.close()

		endTab := tabs.connect()
		tabs.recordProxyActivity()
		endTab()

		// Must stay alive past the 4s idle-tab grace because router traffic happened.
		time.Sleep(tabReconnectGrace + 2*time.Minute)
		synctest.Wait()
		if stopped {
			t.Fatal("active router session should survive opener tab close")
		}

		// Router request resets the 5-minute idle timer.
		tabs.recordProxyActivity()
		time.Sleep(4 * time.Minute)
		synctest.Wait()
		if stopped {
			t.Fatal("router activity should reset the 5-minute idle timer")
		}

		// After 5 full minutes of zero proxy activity, it should auto-stop.
		time.Sleep(1 * time.Minute)
		synctest.Wait()
		if !stopped {
			t.Fatal("bridge should stop after 5 minutes of zero proxy activity")
		}
	})
}

func TestOpenRouterLaunchDoesNotAutoStopBeforeOpenerConnects(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stopped := false
		tabs := newTabLifetime(0, func() { stopped = true })
		defer tabs.close()

		tabs.recordProxyActivity()
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		if stopped {
			t.Fatal("--open-router / --no-browser should stay running until Ctrl+C or an opener connects and closes")
		}
	})
}
