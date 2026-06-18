package ui

import "testing"

func TestCountLabel(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0 campaigns"},
		{1, "1 campaign"},
		{2, "2 campaigns"},
		{42, "42 campaigns"},
	}
	for _, tc := range cases {
		if got := CountLabel(tc.n, "campaign", "campaigns"); got != tc.want {
			t.Errorf("CountLabel(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
