package main

import (
	"sync"
	"time"
)

const (
	openerStartupTimeout = 30 * time.Second
	tabReconnectGrace    = 4 * time.Second
)

type tabLifetime struct {
	mu          sync.Mutex
	tabs        int
	timer       *time.Timer
	closing     bool
	requestStop func()
}

func newTabLifetime(startupTimeout time.Duration, requestStop func()) *tabLifetime {
	lifetime := &tabLifetime{requestStop: requestStop}
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
	l.tabs++
	return l.disconnect
}

func (l *tabLifetime) disconnect() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tabs--
	if l.tabs == 0 && !l.closing {
		l.scheduleStop(tabReconnectGrace)
	}
}

// scheduleStop is called under mu. Comparing timers also handles a callback
// that was already queued when a tab reconnected and stopped its timer.
func (l *tabLifetime) scheduleStop(delay time.Duration) {
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
