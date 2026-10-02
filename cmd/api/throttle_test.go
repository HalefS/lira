package main

import (
	"testing"
	"time"
)

// The throttle is the control that makes the unauthenticated password change
// endpoint safe. These cover the behaviour it depends on: one key is held back,
// other keys are unaffected, and the clock can be moved without sleeping.

func newTestThrottle(now *time.Time) *throttle {
	t := newThrottle()
	t.now = func() time.Time { return *now }
	return t
}

func TestThrottleAllowsTheFirstAttempt(t *testing.T) {
	now := time.Now()
	th := newTestThrottle(&now)

	if ok, wait := th.Allow("lira@lira.local", time.Minute); !ok {
		t.Fatalf("the first attempt for a key was blocked (wait %s); nothing should be blocked yet", wait)
	}
}

func TestThrottleBlocksTheSecondAttemptInsideTheWindow(t *testing.T) {
	now := time.Now()
	th := newTestThrottle(&now)

	th.Allow("lira@lira.local", time.Minute)
	ok, wait := th.Allow("lira@lira.local", time.Minute)
	if ok {
		t.Fatal("the second attempt inside the window was allowed")
	}
	if wait <= 0 || wait > time.Minute {
		t.Fatalf("the remaining wait was %s, want a positive value no more than the window", wait)
	}
}

func TestThrottleIsPerKey(t *testing.T) {
	now := time.Now()
	th := newTestThrottle(&now)

	th.Allow("a@test.local", time.Minute)
	if ok, _ := th.Allow("b@test.local", time.Minute); !ok {
		t.Fatal("a different key was blocked by another key's attempt")
	}
	// The whole point of keying on the address rather than the client: one
	// caller must not be able to lock out somebody else.
	if ok, _ := th.Allow("a@test.local", time.Minute); ok {
		t.Fatal("the first key was not held back")
	}
}

func TestThrottleReleasesAfterTheWindow(t *testing.T) {
	now := time.Now()
	clock := &now
	th := newTestThrottle(clock)

	th.Allow("lira@lira.local", time.Minute)
	if ok, _ := th.Allow("lira@lira.local", time.Minute); ok {
		t.Fatal("blocked too early: the window has not elapsed")
	}

	*clock = (*clock).Add(61 * time.Second)
	if ok, _ := th.Allow("lira@lira.local", time.Minute); !ok {
		t.Fatal("still blocked after the window elapsed")
	}
}

// Keyed on the lowercased address by the caller, because the column is citext
// and LIRA@x.com and lira@x.com are one account.
func TestThrottleTreatsCaseVariantsAsOneKey(t *testing.T) {
	now := time.Now()
	th := newTestThrottle(&now)

	th.Allow("lira@lira.local", time.Minute)
	if ok, _ := th.Allow("LIRA@LIRA.LOCAL", time.Minute); ok {
		t.Fatal("a differently-cased address bypassed the cooldown; the caller must lowercase before calling Allow")
	}
}

func TestThrottlePrunesExpiredKeys(t *testing.T) {
	now := time.Now()
	clock := &now
	th := newTestThrottle(clock)

	for i := 0; i < 50; i++ {
		th.Allow(string(rune('a'+i%26))+string(rune('a'+i/26))+"@test.local", time.Minute)
	}
	before := len(th.byKey)

	// Past the pruning horizon, so every key is collectable.
	*clock = (*clock).Add(2 * time.Hour)
	th.Allow("fresh@test.local", time.Minute)

	if len(th.byKey) >= before && before > 1 {
		t.Fatalf("the map held %d keys before pruning and %d after; old deadlines are not being released", before, len(th.byKey))
	}
}
