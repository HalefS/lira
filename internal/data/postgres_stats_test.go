package data

import "testing"

func TestCacheHitPercent(t *testing.T) {
	tests := []struct {
		name        string
		hits, reads int64
		want        float64
	}{
		{"no reads yet", 0, 0, 0},
		{"everything from disk", 0, 10, 0},
		{"everything from cache", 10, 0, 100},
		{"three quarters", 75, 25, 75},
		{"rounded to one decimal", 2, 3, 40},
	}
	for _, tc := range tests {
		if got := cacheHitPercent(tc.hits, tc.reads); got != tc.want {
			t.Errorf("%s: cacheHitPercent(%d, %d) = %v, want %v",
				tc.name, tc.hits, tc.reads, got, tc.want)
		}
	}
}

func TestCacheHitPercentNeverExceedsRange(t *testing.T) {
	// Counters are cumulative and can theoretically wrap or be reset between
	// reads; a percentage outside 0-100 would make the gauge overflow.
	for _, pair := range [][2]int64{{1, 1}, {99, 1}, {1000000, 1}, {1, 1000000}} {
		got := cacheHitPercent(pair[0], pair[1])
		if got < 0 || got > 100 {
			t.Errorf("cacheHitPercent(%d, %d) = %v, want 0..100", pair[0], pair[1], got)
		}
	}
}
