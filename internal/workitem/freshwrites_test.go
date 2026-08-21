package workitem

import (
	"strings"
	"testing"
	"time"
)

// TestIsFreshlyWritten is the guard's whole decision, exercised against an
// injected clock rather than a real filesystem so the boundary is exact.
func TestIsFreshlyWritten(t *testing.T) {
	now := time.Date(2026, 8, 20, 13, 34, 0, 0, time.UTC)

	tests := []struct {
		name   string
		newest time.Time
		want   bool
	}{
		{
			name:   "no content files at all is not fresh",
			newest: time.Time{},
			want:   false,
		},
		{
			name:   "exactly at the window boundary is not fresh",
			newest: now.Add(-FreshWriteWindow),
			want:   false,
		},
		{
			name:   "just outside the window is not fresh",
			newest: now.Add(-FreshWriteWindow - time.Second),
			want:   false,
		},
		{
			name:   "an hour old is not fresh",
			newest: now.Add(-time.Hour),
			want:   false,
		},
		{
			name:   "written a second ago is fresh",
			newest: now.Add(-time.Second),
			want:   true,
		},
		{
			name:   "just inside the window is fresh",
			newest: now.Add(-FreshWriteWindow + time.Second),
			want:   true,
		},
		{
			name:   "a modification time in the future is fresh (clock skew is not evidence of stopping)",
			newest: now.Add(time.Hour),
			want:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsFreshlyWritten(tc.newest, now); got != tc.want {
				t.Errorf("IsFreshlyWritten(%v, %v) = %v, want %v", tc.newest, now, got, tc.want)
			}
		})
	}
}

func TestFreshWriteDetail(t *testing.T) {
	now := time.Date(2026, 8, 20, 13, 34, 0, 0, time.UTC)

	tests := []struct {
		name   string
		newest time.Time
		want   string
	}{
		{
			name:   "future write names the clock skew instead of a negative age",
			newest: now.Add(time.Minute),
			want:   "last written in the future (clock skew)",
		},
		{
			name:   "one minute is singular",
			newest: now.Add(-time.Minute),
			want:   "last written 1 minute ago",
		},
		{
			name:   "several minutes is plural",
			newest: now.Add(-4 * time.Minute),
			want:   "last written 4 minutes ago",
		},
		{
			name:   "under a minute rounds to zero minutes, still plural",
			newest: now.Add(-2 * time.Second),
			want:   "last written 0 minutes ago",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FreshWriteDetail(tc.newest, now)
			if !strings.Contains(got, tc.want) {
				t.Errorf("FreshWriteDetail(...) = %q, want it to contain %q", got, tc.want)
			}
			if !strings.Contains(got, "may still be writing") {
				t.Errorf("FreshWriteDetail(...) = %q, want it to say why the item was left alone", got)
			}
		})
	}
}
