package workitem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSlug(t *testing.T) {
	cases := []struct {
		slug string
		ok   bool
	}{
		{"foo", true},
		{"foo-bar", true},
		{"foo_bar", true},
		{"foo123", true},
		{"a", true},
		{"", false},
		{"Foo", false},
		{"foo bar", false},
		{"-foo", false},
		{"foo!", false},
		{strings.Repeat("a", 81), false},
	}
	for _, c := range cases {
		err := validateSlug(c.slug)
		if (err == nil) != c.ok {
			t.Errorf("validateSlug(%q) error=%v, want ok=%v", c.slug, err, c.ok)
		}
	}
}

func TestValidateParentPath(t *testing.T) {
	cases := []struct {
		path string
		ok   bool
	}{
		{"workflow/feature", true},
		{"workflow/incident", true},
		{"/abs/path", false},
		{"../escape", false},
	}
	for _, c := range cases {
		err := validateParentPath(c.path)
		if (err == nil) != c.ok {
			t.Errorf("validateParentPath(%q) error=%v, want ok=%v", c.path, err, c.ok)
		}
	}
}

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yaml")
	if err := atomicWriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("contents = %q, want hello", got)
	}
}
