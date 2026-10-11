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
	"unicode/utf8"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
)

func TestExploreReadFailures(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{root, filepath.Join(root, "missing")} {
		if _, _, err := readExploreText(context.Background(), path); err == nil {
			t.Fatalf("reading %s silently succeeded", path)
		}
	}
	path := filepath.Join(root, "goal.md")
	want := strings.Repeat("a", 256<<10) + "beyond limit"
	if err := os.WriteFile(path, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	if got, cut, err := readExploreText(context.Background(), path); err != nil || !cut || got != want[:256<<10] {
		t.Fatalf("bounded read: %d bytes, cut %v, %v", len(got), cut, err)
	}
	m := replayModel(explore.ProtocolOff)
	m.root = root
	m.visible.Items[0].Path = "."
	m.visible.Items[0].IsDir = true
	if err := os.WriteFile(filepath.Join(root, "FESTIVAL_GOAL.md"), []byte("# Goal\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// A goal file that is there and cannot be read: the read is cancelled.
	stopped, stop := context.WithCancel(context.Background())
	stop()
	m.ctx = stopped
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

	// The terminal branch reads the configured theme, and that read creates
	// nothing either. With no global config there is still none afterwards.
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	if got := exploreTheme(context.Background()); got != theme.ThemeAdaptive {
		t.Fatalf("theme with nothing configured = %q", got)
	}
	if _, err := os.Stat(configHome); !os.IsNotExist(err) {
		t.Fatalf("reading the theme created the config home: %v", err)
	}
	globalPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	global := []byte(`{"tui":{"theme":"light"}}` + "\n")
	if err := os.WriteFile(globalPath, global, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := exploreTheme(context.Background()); got != theme.ThemeLight {
		t.Fatalf("theme with light configured = %q", got)
	}
	// The camp's own override wins over the global setting.
	if err := config.SaveLocalSettings(context.Background(), root, &config.LocalSettings{ThemeOverride: config.ThemeNameDark}); err != nil {
		t.Fatal(err)
	}
	local, err := os.ReadFile(config.LocalSettingsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if got := exploreTheme(context.Background()); got != theme.ThemeDark {
		t.Fatalf("theme with a dark override in the camp = %q", got)
	}
	for path, want := range map[string][]byte{globalPath: global, config.LocalSettingsPath(root): local, configPath: original} {
		if got, err := os.ReadFile(path); err != nil || string(got) != string(want) {
			t.Fatalf("reading the theme changed %s: %q, %v", path, got, err)
		}
	}
	if entries, err := os.ReadDir(filepath.Dir(globalPath)); err != nil || len(entries) != 1 {
		t.Fatalf("reading the theme left %d entries beside the global config: %v", len(entries), err)
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

	// A status from an earlier key press, such as a copy, describes the
	// listing. It does not stay over the document that opens next.
	for _, earlier := range []struct {
		text   string
		failed bool
	}{{"copied", false}, {"copy failed: denied", true}} {
		opened := replayModel(explore.ProtocolOff)
		opened.reading = true
		opened.reader = exploreReader{listing: true, entries: []readerEntry{{name: "readme.md", path: good}}}
		opened.setStatus(exploreActionStatus, earlier.text, earlier.failed)
		opened, _ = press(t, opened, tea.KeyMsg{Type: tea.KeyEnter})
		if opened.reader.listing || opened.status != "" || opened.statusErr {
			t.Fatalf("opened document still shows %q err=%v listing=%v", opened.status, opened.statusErr, opened.reader.listing)
		}
	}
}

// A document longer than the reader loads must say that the rest is missing.
func TestExploreReaderSaysWhenADocumentIsCut(t *testing.T) {
	root := t.TempDir()
	// The limit falls one byte into a three-byte character.
	long := "# Long\n\n"
	long += strings.Repeat("word ", readerLimit/5)[:readerLimit-len(long)-1] + "界界界 the end\n"
	small := "# Small\n\nAll of it.\n"
	for name, text := range map[string]string{"long.md": long, "small.md": small, "FESTIVAL_GOAL.md": long} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const notice = "Showing the first 256 KiB of this file."
	text, cut, err := readExploreText(context.Background(), filepath.Join(root, "long.md"))
	if err != nil || !cut || len(text) != readerLimit-1 || !utf8.ValidString(text) || strings.Contains(text, "界") {
		t.Fatalf("long read: %d bytes, cut %v, valid %v, %v", len(text), cut, utf8.ValidString(text), err)
	}
	if text, cut, err := readExploreText(context.Background(), filepath.Join(root, "small.md")); err != nil || cut || text != small {
		t.Fatalf("small read: %q, cut %v, %v", text, cut, err)
	}

	open := func(item explore.Item) exploreModel {
		m := replayModel(explore.ProtocolOff)
		m.root = root
		m.visible.Items[0] = item
		m.cursor = 0
		m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if !m.reading {
			t.Fatalf("enter did not open %s: %q", item.Path, m.status)
		}
		return m
	}
	// The top of the screen, as text.
	top := func(m exploreModel) string {
		lines := strings.Split(m.View(), "\n")
		for i, line := range lines {
			lines[i] = strings.TrimRight(viewText(line), " ")
		}
		return strings.Join(lines[:8], "\n")
	}
	file := open(explore.Item{Kind: explore.KindMarkdown, Title: "long", Path: "long.md"})
	if got := top(file); !strings.Contains(got, notice) || !strings.Contains(got, "Long") {
		t.Fatalf("a cut document does not say so: %q", got)
	}
	whole := open(explore.Item{Kind: explore.KindMarkdown, Title: "small", Path: "small.md"})
	if got := top(whole); strings.Contains(got, notice) || !strings.Contains(got, "All of it.") {
		t.Fatalf("a whole document claims to be cut: %q", got)
	}
	goal := open(explore.Item{Kind: explore.KindFestival, Title: "festival", Path: ".", IsDir: true})
	if got := top(goal); !strings.Contains(got, notice) || !strings.Contains(got, "No replay yet.") {
		t.Fatalf("a cut festival goal lost a notice: %q", got)
	}
	listing := open(explore.Item{Kind: explore.KindWorkitem, Title: "workitem", Path: ".", IsDir: true})
	for i, entry := range listing.reader.entries {
		if entry.name == "long.md" {
			listing.reader.entry = i
		}
	}
	listing, _ = press(t, listing, tea.KeyMsg{Type: tea.KeyEnter})
	if got := top(listing); listing.reader.listing || !strings.Contains(got, notice) {
		t.Fatalf("a cut listing entry does not say so: %q", got)
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

func TestExploreCacheSaveRunsOffTheUpdateLoop(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "dungeon-feed")
	m := replayModel(explore.ProtocolOff)
	m.ctx = ctx
	m.cacheDir = dir
	idx := refreshIndex("Saved")
	m, cmd := press(t, m, exploreLoaded{gen: m.indexGen, index: idx, changed: true})
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("Update wrote the cache itself (%v)", err)
	}
	if cmd == nil {
		t.Fatal("a changed index scheduled no cache save")
	}
	// With images off nothing else is scheduled, so the command is the save.
	if msg := cmd(); msg != nil {
		t.Fatalf("save command sent %T", msg)
	}
	cached, ok, err := explore.LoadCache(ctx, dir)
	if err != nil || !ok || len(cached.Items) != 1 || cached.Items[0].Title != "Saved" {
		t.Fatalf("saved cache: ok %v, err %v, items %+v", ok, err, cached.Items)
	}

	_, cmd = press(t, m, exploreLoaded{gen: m.indexGen, index: idx})
	if cmd != nil {
		t.Fatal("an unchanged index scheduled a cache save")
	}
}
