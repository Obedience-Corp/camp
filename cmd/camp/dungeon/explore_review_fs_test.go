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

func TestExploreJSONDoesNotWriteConfig(t *testing.T) {
	t.Setenv("CAMP_ROOT", "")
	root := t.TempDir()
	t.Chdir(root)
	campaignDir := filepath.Join(root, ".campaign")
	if err := os.Mkdir(campaignDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(campaignDir, "campaign.yaml")
	original := []byte("name: fixture\n")
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd := dungeonExploreCmd
	cmd.SetOut(&out)
	cmd.SetContext(context.Background())
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.SetOut(nil)
		_ = cmd.Flags().Set("json", "false")
	})
	if err := runDungeonExplore(cmd, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("campaign.yaml changed:\n%s", got)
	}
	jumps := filepath.Join(campaignDir, "settings", "jumps.yaml")
	if _, err := os.Stat(jumps); !os.IsNotExist(err) {
		t.Fatalf("explore created jumps.yaml: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("--json did not emit JSON: %s (%v)", out.String(), err)
	}

	// A config that cannot be loaded must still leave the tree untouched.
	broken := []byte("description: missing name\n")
	if err := os.WriteFile(configPath, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runDungeonExplore(cmd, nil); err == nil {
		t.Fatal("missing camp name was accepted")
	}
	got, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(broken) {
		t.Fatalf("failed explore rewrote campaign.yaml:\n%s", got)
	}
	if _, err := os.Stat(jumps); !os.IsNotExist(err) {
		t.Fatalf("failed explore created jumps.yaml: %v", err)
	}
}

func TestExploreReaderClearsParseError(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "readme.md")
	if err := os.WriteFile(good, []byte("# Hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := replayModel(explore.ProtocolOff)
	m.reading = true
	m.reader = exploreReader{
		listing: true,
		entries: []readerEntry{
			{name: "missing.md", path: filepath.Join(root, "missing.md")},
			{name: "readme.md", path: good},
		},
	}
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.statusErr || m.status != "The file could not be parsed." || !m.reader.listing {
		t.Fatalf("unreadable entry status=%q err=%v listing=%v", m.status, m.statusErr, m.reader.listing)
	}
	stuck := m
	m, _ = press(t, m, runes("j"))
	if m.statusErr || m.status != "" || m.reader.entry != 1 {
		t.Fatalf("the failure followed the cursor: status=%q entry=%d", m.status, m.reader.entry)
	}
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.statusErr || m.status != "" || m.reader.listing {
		t.Fatalf("readable document still shows %q err=%v listing=%v", m.status, m.statusErr, m.reader.listing)
	}
	// Opening a readable entry clears the failure even when the cursor did
	// not move, as when the file is fixed and opened again.
	stuck.reader.entries[0].path = good
	stuck, _ = press(t, stuck, tea.KeyMsg{Type: tea.KeyEnter})
	if stuck.statusErr || stuck.status != "" || stuck.reader.listing {
		t.Fatalf("reopened document still shows %q err=%v listing=%v", stuck.status, stuck.statusErr, stuck.reader.listing)
	}
	if strings.Contains(m.View(), "could not be parsed") || !strings.Contains(m.View(), "Hello") {
		t.Fatal("the reader kept the parse error or dropped the document")
	}

	kept := replayModel(explore.ProtocolOff)
	kept.reading = true
	kept.reader = exploreReader{listing: true, entries: []readerEntry{{name: "readme.md", path: good}}}
	kept.setStatus(exploreActionStatus, "copied", false)
	kept, _ = press(t, kept, tea.KeyMsg{Type: tea.KeyEnter})
	if kept.statusErr || kept.status != "copied" || kept.reader.listing {
		t.Fatalf("successful open cleared an unrelated status: %q err=%v listing=%v", kept.status, kept.statusErr, kept.reader.listing)
	}
	kept = replayModel(explore.ProtocolOff)
	kept.reading = true
	kept.reader = exploreReader{listing: true, entries: []readerEntry{{name: "readme.md", path: good}}}
	kept.setStatus(exploreActionStatus, "copy failed: denied", true)
	kept, _ = press(t, kept, tea.KeyMsg{Type: tea.KeyEnter})
	if !kept.statusErr || kept.status != "copy failed: denied" || kept.reader.listing {
		t.Fatalf("successful open cleared an unrelated error: %q err=%v listing=%v", kept.status, kept.statusErr, kept.reader.listing)
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
