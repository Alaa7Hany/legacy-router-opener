package main

import (
	"sync"
	"time"
)

const (
	openerStartupTimeout = 30 * time.Second
	tabReconnectGrace    = 4 * time.Second
	proxyIdleTimeout     = 5 * time.Minute
)

type tabLifetime struct {
	mu                sync.Mutex
	tabs              int
	timer             *time.Timer
	closing           bool
	sawOpenerTab      bool
	hasStartupTimeout bool
	hasProxyActivity  bool
	lastProxyActivity time.Time
	reconnectGrace    time.Duration
	proxyIdleGrace    time.Duration
	requestStop       func()
}

func newTabLifetime(startupTimeout time.Duration, requestStop func()) *tabLifetime {
	lifetime := &tabLifetime{
		hasStartupTimeout: startupTimeout > 0,
		reconnectGrace:    tabReconnectGrace,
		proxyIdleGrace:    proxyIdleTimeout,
		requestStop:       requestStop,
	}
	if startupTimeout > 0 {
		lifetime.mu.Lock()
		lifetime.scheduleStop(startupTimeout)
		lifetime.mu.Unlock()
	}
	return lifetime
}

// connect returns the function to call when this tab's event stream ends.
func (l *tabLifetime) connect() func() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closing {
		return func() {}
	}
	l.cancelTimer()
	l.sawOpenerTab = true
	l.tabs++
	return l.disconnect
}

func (l *tabLifetime) disconnect() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tabs--
	if l.tabs == 0 && !l.closing {
		delay := l.reconnectGrace
		if l.hasProxyActivity {
			delay = l.proxyIdleGrace
		}
		l.scheduleStop(delay)
	}
}

// recordProxyActivity keeps active router sessions alive for proxyIdleGrace after the opener tab closes.
func (l *tabLifetime) recordProxyActivity() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closing {
		return
	}
	l.hasProxyActivity = true
	l.lastProxyActivity = time.Now()
	if l.tabs == 0 && (l.sawOpenerTab || l.hasStartupTimeout) {
		l.scheduleStop(l.proxyIdleGrace)
	}
}

// scheduleStop is called under mu. Comparing timers also handles a callback
// that was already queued when a tab reconnected and stopped its timer.
func (l *tabLifetime) scheduleStop(delay time.Duration) {
	l.cancelTimer()
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.timer != timer {
			return
		}
		l.timer = nil
		l.closing = true
		l.requestStop()
	})
	l.timer = timer
}

func (l *tabLifetime) cancelTimer() {
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
}

func (l *tabLifetime) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closing = true
	l.cancelTimer()
}
