package upstream

import (
	"context"
	"sync"
)

// slotLimiter bounds concurrent upstream sockets cleanly and supports live preset limit updates.
type slotLimiter struct {
	mu     sync.Mutex
	cond   *sync.Cond
	limit  int
	active int
}

// newSlotLimiter creates a concurrency limiter with the specified capacity (>= 1).
func newSlotLimiter(limit int) *slotLimiter {
	if limit < 1 {
		limit = 1
	}
	s := &slotLimiter{limit: limit}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// SetLimit adjusts the maximum number of concurrent slots and wakes waiting callers.
func (s *slotLimiter) SetLimit(limit int) {
	if limit < 1 {
		limit = 1
	}
	s.mu.Lock()
	s.limit = limit
	s.cond.Broadcast()
	s.mu.Unlock()
}

// Limit returns the current concurrency cap.
func (s *slotLimiter) Limit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limit
}

// Acquire blocks until a slot is available or ctx is cancelled.
func (s *slotLimiter) Acquire(ctx context.Context) error {
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.cond.Broadcast()
			s.mu.Unlock()
		case <-done:
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()
	for s.active >= s.limit {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.cond.Wait()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.active++
	return nil
}

// Release frees one slot and wakes a waiting caller.
func (s *slotLimiter) Release() {
	s.mu.Lock()
	if s.active > 0 {
		s.active--
	}
	s.cond.Signal()
	s.mu.Unlock()
}
