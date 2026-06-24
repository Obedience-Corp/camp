package pathutil

import (
	"path/filepath"
	"testing"
)

func TestAbbreviateHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"under home", filepath.Join(home, "Dev", "x"), "~" + string(filepath.Separator) + filepath.Join("Dev", "x")},
		{"exact home", home, "~"},
		{"not under home", "/tmp/x", "/tmp/x"},
		{"sibling prefix", home + "-other/x", home + "-other/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AbbreviateHome(tc.in); got != tc.want {
				t.Errorf("AbbreviateHome(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAbbreviateHome_EmptyHome(t *testing.T) {
	t.Setenv("HOME", "")
	if got := AbbreviateHome("/some/path"); got != "/some/path" {
		t.Errorf("with empty HOME, AbbreviateHome should be a no-op, got %q", got)
	}
}
