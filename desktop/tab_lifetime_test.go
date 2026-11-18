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
