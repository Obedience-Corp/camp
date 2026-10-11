//go:build container_fs

package explore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

// The index reads FESTIVAL_GOAL.md only when it is a regular file. The reader
// must agree: a link, a directory, or a pipe of that name is a festival with
// no goal file, and Enter still opens it on the summary.
func TestExploreReaderOpensFestivalWithoutARegularGoalFile(t *testing.T) {
	stage := map[string]func(t *testing.T, goal string){
		"symlink": func(t *testing.T, goal string) {
			target := filepath.Join(filepath.Dir(filepath.Dir(goal)), "shared-goal.md")
			if err := os.WriteFile(target, []byte("# Linked\n\nText from outside the festival.\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, goal); err != nil {
				t.Fatal(err)
			}
		},
		"directory": func(t *testing.T, goal string) {
			if err := os.Mkdir(goal, 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"pipe": func(t *testing.T, goal string) {
			if err := unix.Mkfifo(goal, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, put := range stage {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			festival := filepath.Join(root, "festival")
			if err := os.Mkdir(festival, 0o700); err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(festival, "FESTIVAL_GOAL.md"))

			m := replayModel(intexplore.ProtocolOff)
			m.root = root
			m.visible.Items[0].Path = "festival"
			m.visible.Items[0].IsDir = true
			summary := m.visible.Items[0].Summary
			next, _ := m.openReader()
			m = next.(exploreModel)
			if !m.reading || m.statusErr {
				t.Fatalf("Enter did not open the festival: reading %v, status %q", m.reading, m.status)
			}
			text := strings.Join(m.reader.lines, "\n")
			if !strings.Contains(strings.Join(strings.Fields(text), " "), summary) {
				t.Fatalf("reader does not show the summary: %q", text)
			}
			if strings.Contains(text, "outside the festival") {
				t.Fatalf("reader followed the link: %q", text)
			}

			m.visible.Items[0].Summary = ""
			next, _ = m.openReader()
			if got := strings.Join(next.(exploreModel).reader.lines, "\n"); !strings.Contains(got, "No festival goal file.") {
				t.Fatalf("reader with no summary = %q", got)
			}
		})
	}
}
