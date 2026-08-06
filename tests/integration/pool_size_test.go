//go:build integration
// +build integration

package integration

import (
	"runtime"
	"testing"
)

// The pool size decides how many containers run at once, and getting it wrong
// does not announce itself: an oversubscribed daemon produces tests that pass
// alone and fail together, differently each run, which reads like flaky tests.
// So the arithmetic is worth pinning even though it is four lines.
func TestPoolSizeFor(t *testing.T) {
	t.Setenv("CAMP_TEST_POOL_SIZE", "")

	tests := []struct {
		name       string
		daemonCPUs int
		want       int
	}{
		{
			// Both prior sizings were wrong on a 4-CPU Colima VM behind a
			// 16-CPU laptop: sizing from the laptop gave six containers
			// (oversubscribed, flaky), halving the daemon's own count gave
			// two (a 3x wall-clock regression). One per daemon CPU is the
			// point between them.
			name:       "a four cpu daemon gets four",
			daemonCPUs: 4,
			want:       4,
		},
		{
			name:       "a large daemon is still capped",
			daemonCPUs: 64,
			want:       6,
		},
		{
			name:       "a tiny daemon still gets the floor",
			daemonCPUs: 1,
			want:       2,
		},
		{
			name:       "twelve cpus lands on the cap",
			daemonCPUs: 12,
			want:       6,
		},
		{
			name:       "five cpus lands inside the range",
			daemonCPUs: 5,
			want:       5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := poolSizeFor(tt.daemonCPUs); got != tt.want {
				t.Fatalf("poolSizeFor(%d) = %d, want %d", tt.daemonCPUs, got, tt.want)
			}
		})
	}
}

// An unreadable daemon must not make the pool worse than it was. Falling back
// to the host count is exactly the old behavior, so a daemon that will not
// answer costs nothing beyond the bug this fix removes.
func TestPoolSizeForFallsBackToTheHost(t *testing.T) {
	t.Setenv("CAMP_TEST_POOL_SIZE", "")

	want := runtime.NumCPU() / 2
	if want < 2 {
		want = 2
	}
	if want > 6 {
		want = 6
	}
	for _, unknown := range []int{0, -1} {
		if got := poolSizeFor(unknown); got != want {
			t.Errorf("poolSizeFor(%d) = %d, want the host-derived %d",
				unknown, got, want)
		}
	}
}

// The override is how someone with a bigger daemon, or a wedged one, takes
// control. It has to beat whatever the daemon reports.
func TestPoolSizeForHonorsTheOverride(t *testing.T) {
	t.Setenv("CAMP_TEST_POOL_SIZE", "9")
	if got := poolSizeFor(4); got != 9 {
		t.Fatalf("poolSizeFor(4) with an override of 9 = %d, want 9", got)
	}

	// A meaningless override is ignored rather than obeyed into a zero-sized
	// pool, which would deadlock every checkout.
	t.Setenv("CAMP_TEST_POOL_SIZE", "banana")
	if got := poolSizeFor(4); got != 4 {
		t.Fatalf("poolSizeFor(4) with a junk override = %d, want the computed 4", got)
	}
}
