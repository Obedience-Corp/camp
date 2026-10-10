//go:build container_fs

package dungeon

import (
	"bytes"
	"context"
	"encoding/json"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
)

func TestExploreReadFailures(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{root, filepath.Join(root, "missing")} {
		if _, err := readExploreText(context.Background(), path); err == nil {
			t.Fatalf("reading %s silently succeeded", path)
		}
	}
	path := filepath.Join(root, "goal.md")
	want := strings.Repeat("a", 256<<10) + "beyond limit"
	if err := os.WriteFile(path, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := readExploreText(context.Background(), path); err != nil || got != want[:256<<10] {
		t.Fatalf("bounded read: %d bytes, %v", len(got), err)
	}
	m := replayModel(explore.ProtocolOff)
	m.root = root
	m.visible.Items[0].Path = "."
	m.visible.Items[0].IsDir = true
	if err := os.Mkdir(filepath.Join(root, "FESTIVAL_GOAL.md"), 0700); err != nil {
		t.Fatal(err)
	}
	next, _ := m.openReader()
	m = next.(exploreModel)
	if m.reading || !m.statusErr {
		t.Fatal("unreadable goal opened a blank reader")
	}
}

func TestExplorePipedInputJSON(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Mkdir(filepath.Join(root, ".campaign"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"), []byte("id: fixture\nname: fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	_ = writer.Close()
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = input, slave
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	if exploreTerminal() {
		t.Fatal("pipe stdin selected interactive mode")
	}
	var out bytes.Buffer
	cmd := dungeonExploreCmd
	cmd.SetOut(&out)
	cmd.SetContext(context.Background())
	defer cmd.SetOut(nil)
	if err := runDungeonExplore(cmd, nil); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("pipe did not emit JSON: %s (%v)", out.String(), err)
	}
	if result["schema_version"] != explore.SchemaVersion {
		t.Fatalf("wrong result: %+v", result)
	}
}

func TestExploreReaderRejectsSpecialFiles(t *testing.T) {
	root := t.TempDir()
	normal := filepath.Join(root, "README.md")
	if err := os.WriteFile(normal, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "pending.md")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(normal, filepath.Join(root, "link.md")); err != nil {
		t.Fatal(err)
	}
	entries, err := markdownEntries(root)
	if err != nil || len(entries) != 1 || entries[0].name != "README.md" {
		t.Fatalf("entries: %+v, %v", entries, err)
	}
	// A file selected in a previous directory listing may have been replaced.
	m := replayModel(explore.ProtocolOff)
	m.reading = true
	m.reader = exploreReader{listing: true, entries: []readerEntry{{name: "pending.md", path: fifo}}}
	done := make(chan exploreModel, 1)
	go func() { next, _ := m.onReaderKey(tea.KeyMsg{Type: tea.KeyEnter}); done <- next.(exploreModel) }()
	select {
	case next := <-done:
		if !next.statusErr || !next.reader.listing {
			t.Fatal("special file opened in reader")
		}
	case <-time.After(time.Second):
		t.Fatal("reader blocked on FIFO")
	}
}
