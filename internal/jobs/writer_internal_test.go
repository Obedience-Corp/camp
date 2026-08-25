package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCapturedParentHEAD(t *testing.T) {
	parent := "cafebabecafebabecafebabecafebabecafebabe"
	cases := []struct {
		name   string
		parent string
		want   string
	}{
		{name: "unborn empty", parent: "", want: scratchUnbornHEAD},
		{name: "unborn whitespace", parent: " \t", want: scratchUnbornHEAD},
		{name: "detached parent", parent: parent, want: parent + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := capturedParentHEAD(tc.parent)
			if got != tc.want {
				t.Fatalf("capturedParentHEAD(%q) = %q, want %q", tc.parent, got, tc.want)
			}
		})
	}
}

func TestScratchUnbornHEADIsASymbolicRef(t *testing.T) {
	if !strings.HasPrefix(scratchUnbornHEAD, "ref: refs/") {
		t.Fatalf("unborn scratch HEAD must be a symbolic ref git will accept, got %q", scratchUnbornHEAD)
	}
	if !strings.HasSuffix(scratchUnbornHEAD, "\n") {
		t.Fatal("HEAD file contents must end in a newline")
	}
	// A blank HEAD file makes git reject GIT_DIR as "not a git repository",
	// which is how an empty Job.Parent used to fail every unborn --auto-write.
	if strings.TrimSpace(scratchUnbornHEAD) == "" {
		t.Fatal("unborn scratch HEAD must not be empty")
	}
}

func TestPinCapturedParentWritesUnbornHEAD(t *testing.T) {
	dir := t.TempDir()
	if err := pinCapturedParent(dir, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != scratchUnbornHEAD {
		t.Fatalf("HEAD = %q, want %q", got, scratchUnbornHEAD)
	}
}
