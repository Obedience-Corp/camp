package festivals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/config/registryfile"
	festdetect "github.com/Obedience-Corp/camp/internal/fest"
	"github.com/Obedience-Corp/camp/internal/ui"
)

func newTestFestivalsCmd() *cobra.Command {
	c := &cobra.Command{Use: "festivals"}
	c.Flags().Bool("json", false, "")
	c.Flags().BoolP("interactive", "i", false, "")
	c.Flags().String("org", "", "")
	c.Flags().StringSlice("tag", nil, "")
	c.Flags().String("status", "", "")
	c.Flags().Bool("all", false, "")
	c.Flags().Bool("all-campaigns", false, "")
	c.Flags().String("since", "", "")
	c.Flags().String("until", "", "")
	c.Flags().String("sort", "", "")
	return c
}

func TestFestivalsTUIRequested(t *testing.T) {
	t.Run("json disables", func(t *testing.T) {
		c := newTestFestivalsCmd()
		_ = c.Flags().Set("json", "true")
		if festivalsTUIRequested(c, true) {
			t.Error("--json must not open the TUI")
		}
	})

	t.Run("shaping flags disable", func(t *testing.T) {
		strFlags := []string{"org", "tag", "status", "since", "until", "sort"}
		boolFlags := []string{"all", "all-campaigns"}
		for _, f := range strFlags {
			c := newTestFestivalsCmd()
			_ = c.Flags().Set(f, "x")
			if festivalsTUIRequested(c, true) {
				t.Errorf("%s changed must not open the TUI", f)
			}
		}
		for _, f := range boolFlags {
			c := newTestFestivalsCmd()
			_ = c.Flags().Set(f, "true")
			if festivalsTUIRequested(c, true) {
				t.Errorf("%s changed must not open the TUI", f)
			}
		}
	})

	t.Run("interactive forces even without tty", func(t *testing.T) {
		c := newTestFestivalsCmd()
		_ = c.Flags().Set("interactive", "true")
		if !festivalsTUIRequested(c, false) {
			t.Error("-i must force the TUI")
		}
	})

	t.Run("bare tty opens", func(t *testing.T) {
		if !festivalsTUIRequested(newTestFestivalsCmd(), true) {
			t.Error("bare TTY with no flags should open the TUI")
		}
	})

	t.Run("non tty falls to text", func(t *testing.T) {
		if festivalsTUIRequested(newTestFestivalsCmd(), false) {
			t.Error("non-TTY must not open the TUI")
		}
	})
}

func TestRunFestivalsTUI_AggregateErrorBeforeOpen(t *testing.T) {
	orig := festCLILookup
	t.Cleanup(func() { festCLILookup = orig })
	festCLILookup = func() (string, error) { return "", errors.New("no fest") }

	campPath := campaignWithFestivals(t)
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", registryPath)
	file := registryfile.File{
		Version: 1,
		Campaigns: map[string]registryfile.Campaign{
			"x": {Name: "alpha", Path: campPath, Org: "obey", Status: "active"},
		},
	}
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := os.WriteFile(registryPath, data, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	cmd := newTestFestivalsCmd()
	cmd.Flags().String("path-output", "", "")
	cmd.SetContext(context.Background())

	if err := runFestivalsTUI(cmd); err == nil {
		t.Fatal("expected the aggregation error to surface before the TUI opens")
	}
}

func TestRunFestivalsTUI_NonTTYFallsBackToText(t *testing.T) {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		t.Skip("stdout is a TTY")
	}
	festPath := writeFakeFest(t, fakeFestSuccess)
	festdetect.ResetCache()
	t.Cleanup(festdetect.ResetCache)
	t.Setenv("PATH", filepath.Dir(festPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	campPath := campaignWithFestivals(t)
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", registryPath)
	file := registryfile.File{
		Version: 1,
		Campaigns: map[string]registryfile.Campaign{
			"x": {Name: "alpha", Path: campPath, Org: "obey", Status: "active"},
		},
	}
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := os.WriteFile(registryPath, data, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	cmd := newTestFestivalsCmd()
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runFestivalsTUI(cmd); err != nil {
		t.Fatalf("fallback render: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "f1") || !strings.Contains(got, "alpha") {
		t.Fatalf("expected text renderer output, got:\n%s", got)
	}
}

func TestSortFestivals_OrgCampaignName(t *testing.T) {
	in := []festivalItem{
		{Org: "b", Campaign: "y", Festival: "2"},
		{Org: "a", Campaign: "x", Festival: "2"},
		{Org: "a", Campaign: "x", Festival: "1"},
	}
	got := sortFestivals(in)
	if got[0].Festival != "1" || got[1].Festival != "2" || got[2].Org != "b" {
		t.Errorf("unexpected order: %+v", got)
	}
}

func TestRebuildVisible_ActiveOnly(t *testing.T) {
	m := newFestivalsTUIModel(context.Background(), "a", []festivalItem{
		{Org: "a", Campaign: "x", Festival: "1", Status: "active"},
		{Org: "a", Campaign: "x", Festival: "2", Status: "planning"},
	})
	m.activeOnly = true
	m.rebuildVisible()
	if len(m.visible) != 1 || m.visible[0].Status != "active" {
		t.Errorf("active-only filter wrong: %+v", m.visible)
	}
}

func keyMsg(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestKeymap_NavWrapAndGoto(t *testing.T) {
	items := []festivalItem{
		{Org: "a", Campaign: "x", Festival: "1", Status: "active", Path: "/p/1"},
		{Org: "a", Campaign: "x", Festival: "2", Status: "active", Path: "/p/2"},
	}
	m := newFestivalsTUIModel(context.Background(), "a", items)
	m.gotoEnabled = true

	m2, _ := m.updateBrowse(keyMsg("j"))
	if m2.(festivalsTUIModel).cursor != 1 {
		t.Fatal("j should move to 1")
	}
	m3, _ := m2.(festivalsTUIModel).updateBrowse(keyMsg("j"))
	if m3.(festivalsTUIModel).cursor != 0 {
		t.Fatal("j should wrap to 0")
	}
	m4, cmd := m3.(festivalsTUIModel).updateBrowse(keyMsg("g"))
	fm := m4.(festivalsTUIModel)
	if fm.gotoPath != "/p/1" || !fm.quitting || cmd == nil {
		t.Fatalf("g should record %q and quit, got path=%q quit=%v", "/p/1", fm.gotoPath, fm.quitting)
	}
}

func TestKeymap_GotoHintWhenDisabled(t *testing.T) {
	m := newFestivalsTUIModel(context.Background(), "a", []festivalItem{{Festival: "1", Path: "/p/1"}})
	m2, _ := m.updateBrowse(keyMsg("g"))
	fm := m2.(festivalsTUIModel)
	if fm.quitting || !fm.statusErr {
		t.Fatal("g without shell integration should hint, not quit")
	}
}

func TestKeymap_CopyUsesInjectedClipboard(t *testing.T) {
	orig := ui.WriteClipboard
	t.Cleanup(func() { ui.WriteClipboard = orig })
	var got string
	ui.WriteClipboard = func(s string) error { got = s; return nil }
	m := newFestivalsTUIModel(context.Background(), "a", []festivalItem{{Festival: "1", Path: "/p/1"}})
	m.updateBrowse(keyMsg("y"))
	if got != "/p/1" {
		t.Fatalf("copy should send absolute path, got %q", got)
	}
}

func TestWriteGotoSelection(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "sel")

	// selection present -> writes the absolute path
	m := festivalsTUIModel{gotoPath: "/abs/festivals/active/foo-FA0001"}
	if err := writeGotoSelection(m, out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != "/abs/festivals/active/foo-FA0001" {
		t.Fatalf("wrote %q", string(b))
	}

	// empty gotoPath -> no write
	out2 := filepath.Join(dir, "none")
	if err := writeGotoSelection(festivalsTUIModel{}, out2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out2); !os.IsNotExist(err) {
		t.Fatal("empty gotoPath should not create the file")
	}

	// empty pathOutput -> no-op
	if err := writeGotoSelection(m, ""); err != nil {
		t.Fatal(err)
	}
}
