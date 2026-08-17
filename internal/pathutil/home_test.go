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

func TestHome(t *testing.T) {
	want := t.TempDir()
	t.Setenv("HOME", want)

	got, err := Home()
	if err != nil {
		t.Fatalf("Home() error = %v", err)
	}
	if got != want {
		t.Errorf("Home() = %q, want %q", got, want)
	}
}

// os.UserHomeDir returns ("", err) for an unset HOME, and callers that ignored
// that error joined state paths onto "" and silently wrote them relative to the
// working directory. Home() must surface the failure instead.
func TestHome_UnsetIsAnError(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	got, err := Home()
	if err == nil {
		t.Fatalf("Home() = %q, want an error when HOME is unset", got)
	}
	if got != "" {
		t.Errorf("Home() = %q, want empty string alongside the error", got)
	}
}

// A HOME of only whitespace is not a usable directory either, and os.UserHomeDir
// hands it back as a valid answer.
func TestHome_BlankIsAnError(t *testing.T) {
	t.Setenv("HOME", "   ")
	t.Setenv("USERPROFILE", "   ")

	if got, err := Home(); err == nil {
		t.Fatalf("Home() = %q, want an error for a whitespace-only HOME", got)
	}
}

func TestAbbreviateHome_EmptyHome(t *testing.T) {
	t.Setenv("HOME", "")
	if got := AbbreviateHome("/some/path"); got != "/some/path" {
		t.Errorf("with empty HOME, AbbreviateHome should be a no-op, got %q", got)
	}
}
