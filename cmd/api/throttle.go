package main

import (
	"strings"
	"sync"
	"time"
)

// A throttle is a set of per-key deadlines: a key may act again only once its
// deadline has passed.
//
// It exists for the password change endpoint, where the global rate limiter is
// the wrong shape. That limiter buckets by client, so one caller making
// requests in a tight loop is throttled -- but it cannot stop a single caller
// from guessing the same account's password over and over from one connection.
// Keying on the account instead closes that.
//
// Deadlines are pruned when the map is touched, so an abandoned key cannot leak.
type throttle struct {
	mu    sync.Mutex
	byKey map[string]time.Time
	now   func() time.Time
}

func newThrottle() *throttle {
	return &throttle{
		byKey: make(map[string]time.Time),
		now:   time.Now,
	}
}

// Allow records an attempt for key and reports whether it may proceed.
//
// The key is lowercased first. That matters here because the key is an email
// address and the column behind it is citext: LIRA@x.com and lira@x.com are
// one account, and a case-sensitive map would let the cooldown be sidestepped
// by nothing more than capitalising half the address.
//
// When blocked it also returns how long is left, so the caller can say so
// rather than returning a bare rejection.
func (t *throttle) Allow(key string, wait time.Duration) (bool, time.Duration) {
	key = strings.ToLower(key)

	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	until, seen := t.byKey[key]
	if seen && now.Before(until) {
		return false, until.Sub(now)
	}

	t.byKey[key] = now.Add(wait)
	t.prune(now)
	return true, 0
}

// prune drops keys whose deadline has passed, plus anything already well past
// it. Called with the lock held.
func (t *throttle) prune(now time.Time) {
	// Anything that expired more than an hour ago is kept no longer than needed;
	// the horizon only has to be longer than the longest wait ever configured.
	horizon := now.Add(-time.Hour)
	for k, deadline := range t.byKey {
		if deadline.Before(horizon) {
			delete(t.byKey, k)
		}
	}
}
