package shellcmd

import "testing"

func TestQuoteLiteralForEachShell(t *testing.T) {
	cases := []struct{ goos, in, want string }{
		{"linux", "it's here.mp4", `'it'\''s here.mp4'`},
		{"darwin", "$HOME/take.mov", `'$HOME/take.mov'`},
		{"windows", "it's here.mp4", "'it''s here.mp4'"},
		{"windows", "$env:USERPROFILE `n.mov", "'$env:USERPROFILE `n.mov'"},
	}
	for _, tc := range cases {
		if got := quoteLiteralFor(tc.goos, tc.in); got != tc.want {
			t.Errorf("quoteLiteralFor(%s, %q) = %q, want %q", tc.goos, tc.in, got, tc.want)
		}
	}
}
