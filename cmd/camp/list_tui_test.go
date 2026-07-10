package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/ui"
)

const listFixture = `{
  "version": 2,
  "campaigns": {
    "A-1": {"name":"alpha","path":"/tmp/a","type":"product","org":"default","status":"active","last_access":"2026-06-16T10:00:00Z"},
    "B-2": {"name":"beta","path":"/tmp/b","type":"product","org":"obey","status":"active","last_access":"2026-06-16T10:00:00Z"},
    "C-3": {"name":"gamma","path":"/tmp/c","type":"product","org":"obey","status":"inactive","last_access":"2026-06-16T10:00:00Z"}
  }
}`

func setListRegistry(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", path)
	if err := os.WriteFile(path, []byte(listFixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func newTestListModel(t *testing.T) listTUIModel {
	t.Helper()
	setListRegistry(t)
	reg, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return newListTUIModel(context.Background(), reg)
}

func lkey(m listTUIModel, s string) listTUIModel {
	var msg tea.KeyMsg
	switch s {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	next, _ := m.Update(msg)
	return next.(listTUIModel)
}

func campaignStatus(t *testing.T, id string) string {
	t.Helper()
	reg, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return reg.Campaigns[id].Status
}

func campaignOrg(t *testing.T, id string) string {
	t.Helper()
	reg, _ := config.LoadRegistry(context.Background())
	return reg.Campaigns[id].Org
}

func listTestCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("format", "table", "")
	c.Flags().BoolP("interactive", "i", false, "")
	c.Flags().String("sort", "accessed", "")
	c.Flags().String("org", "", "")
	c.Flags().StringSlice("tag", nil, "")
	c.Flags().String("status", "", "")
	c.Flags().Bool("all", false, "")
	c.Flags().Bool("group", false, "")
	c.Flags().Bool("no-group", false, "")
	c.Flags().Bool("remote", false, "")
	return c
}

func TestListTUIRequested(t *testing.T) {
	defer func() { listJSON, listCount = false, false }()

	t.Run("tty no flags opens", func(t *testing.T) {
		listJSON, listCount = false, false
		if !listTUIRequested(listTestCmd(), true) {
			t.Error("bare camp list in a TTY should open the browser")
		}
	})
	t.Run("piped no flags prints", func(t *testing.T) {
		listJSON, listCount = false, false
		if listTUIRequested(listTestCmd(), false) {
			t.Error("piped camp list should print the table")
		}
	})
	t.Run("json never opens", func(t *testing.T) {
		listJSON, listCount = true, false
		if listTUIRequested(listTestCmd(), true) {
			t.Error("--json must never open the browser")
		}
	})
	t.Run("count never opens", func(t *testing.T) {
		listJSON, listCount = false, true
		if listTUIRequested(listTestCmd(), true) {
			t.Error("--count must never open the browser")
		}
	})
	t.Run("non-table format never opens", func(t *testing.T) {
		listJSON, listCount = false, false
		c := listTestCmd()
		_ = c.Flags().Set("format", "simple")
		if listTUIRequested(c, true) {
			t.Error("--format simple must not open the browser")
		}
	})
	t.Run("interactive forces even when piped", func(t *testing.T) {
		listJSON, listCount = false, false
		c := listTestCmd()
		_ = c.Flags().Set("interactive", "true")
		if !listTUIRequested(c, false) {
			t.Error("-i should request the browser even when piped")
		}
	})
	t.Run("shaping flag in a TTY prints table", func(t *testing.T) {
		listJSON, listCount = false, false
		c := listTestCmd()
		_ = c.Flags().Set("org", "obey")
		if listTUIRequested(c, true) {
			t.Error("a shaping flag should print the table, not open the browser")
		}
	})
	t.Run("remote in a TTY forces the fan-out path", func(t *testing.T) {
		listJSON, listCount = false, false
		c := listTestCmd()
		_ = c.Flags().Set("remote", "true")
		if listTUIRequested(c, true) {
			t.Error("--remote must force the text fan-out path, not open the local browser")
		}
	})
}

func TestLoadVerifiedListRegistry_RepairsBeforeBrowser(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.json")
	campaignRoot := filepath.Join(dir, "actual")
	if err := os.MkdirAll(filepath.Join(campaignRoot, ".campaign"), 0o755); err != nil {
		t.Fatalf("create campaign dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(campaignRoot, ".campaign", "campaign.yaml"),
		[]byte("id: actual-id\nname: fresh-name\ntype: research\n"), 0o644); err != nil {
		t.Fatalf("write campaign config: %v", err)
	}

	t.Setenv("CAMP_REGISTRY_PATH", registryPath)
	registry := `{
  "version": 2,
  "campaigns": {
    "wrong-id": {"name":"stale-name","path":` + strconv.Quote(campaignRoot) + `,"type":"product","status":"active"},
    "missing-id": {"name":"missing","path":` + strconv.Quote(filepath.Join(dir, "missing")) + `,"type":"product","status":"active"}
  }
}`
	if err := os.WriteFile(registryPath, []byte(registry), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	reg, report, err := loadVerifiedListRegistry(context.Background())
	if err != nil {
		t.Fatalf("loadVerifiedListRegistry: %v", err)
	}
	if !report.HasChanges() {
		t.Fatal("expected registry repair changes")
	}
	if _, ok := reg.Campaigns["wrong-id"]; ok {
		t.Fatal("wrong ID should be removed")
	}
	if _, ok := reg.Campaigns["missing-id"]; ok {
		t.Fatal("missing path should be removed")
	}
	if got := reg.Campaigns["actual-id"]; got.Name != "fresh-name" || got.Type != config.CampaignTypeResearch {
		t.Fatalf("actual campaign not repaired from campaign.yaml: %+v", got)
	}

	persisted, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	if _, ok := persisted.Campaigns["actual-id"]; !ok {
		t.Fatal("repaired registry was not persisted")
	}
}

func TestListTUI_OpensOrgMajorAllStatuses(t *testing.T) {
	m := newTestListModel(t)
	if len(m.visible) != 3 {
		t.Fatalf("visible = %d, want 3 (all statuses by default)", len(m.visible))
	}
	if m.visible[0].Name != "alpha" {
		t.Errorf("first row = %q, want alpha (default org first)", m.visible[0].Name)
	}
}

func TestListTUI_CycleStatus_Deactivate(t *testing.T) {
	m := newTestListModel(t)
	m = lkey(m, "s")
	if m.statusErr {
		t.Fatalf("unexpected error: %s", m.status)
	}
	if got := campaignStatus(t, "A-1"); got != "inactive" {
		t.Errorf("alpha status = %q, want inactive after one cycle", got)
	}
	if len(m.visible) != 3 {
		t.Error("deactivated campaign should stay visible in show-all mode")
	}
}

func TestListTUI_Filter_HidesInactive(t *testing.T) {
	m := newTestListModel(t)
	m = lkey(m, "f")
	for _, e := range m.visible {
		if e.Status != "active" {
			t.Errorf("active-only filter still shows %q (%s)", e.Name, e.Status)
		}
	}
	if len(m.visible) != 2 {
		t.Errorf("active-only visible = %d, want 2", len(m.visible))
	}
	m = lkey(m, "f")
	if len(m.visible) != 3 {
		t.Errorf("show-all visible = %d, want 3", len(m.visible))
	}
}

func TestListTUI_MoveOrg(t *testing.T) {
	m := newTestListModel(t)
	m = lkey(m, "m")
	if m.overlay != listOverlayMove {
		t.Fatal("expected move overlay")
	}
	m = lkey(m, "newco")
	m = lkey(m, "enter")
	if m.statusErr {
		t.Fatalf("unexpected error: %s", m.status)
	}
	if got := campaignOrg(t, "A-1"); got != "newco" {
		t.Errorf("alpha org = %q, want newco", got)
	}
}

func TestListTUI_MoveOrg_InvalidName_NoMutation(t *testing.T) {
	m := newTestListModel(t)
	m = lkey(m, "m")
	m = lkey(m, "Bad Org")
	m = lkey(m, "enter")
	if !m.statusErr {
		t.Errorf("expected error status for invalid org name, got %q", m.status)
	}
	if got := campaignOrg(t, "A-1"); got != "default" {
		t.Errorf("alpha org = %q, want default (unchanged)", got)
	}
}

func TestListTUI_CopyPath_UsesTilde(t *testing.T) {
	prev := ui.WriteClipboard
	var copied string
	ui.WriteClipboard = func(s string) error { copied = s; return nil }
	defer func() { ui.WriteClipboard = prev }()

	m := newTestListModel(t)
	m = lkey(m, "y")
	if m.statusErr {
		t.Fatalf("copy failed: %s", m.status)
	}
	if copied != "/tmp/a" {
		t.Errorf("copied %q, want /tmp/a (fixture path, not under home)", copied)
	}
	if m.status != "copied!" {
		t.Errorf("status = %q, want copied!", m.status)
	}
}

func TestListTUI_Quit(t *testing.T) {
	m := newTestListModel(t)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !next.(listTUIModel).quitting {
		t.Error("q should set quitting")
	}
	if cmd == nil {
		t.Error("q should return a quit command")
	}
}

func TestListTUI_ViewSmoke(t *testing.T) {
	m := newTestListModel(t)
	m.width, m.height = 100, 30
	out := m.View()
	for _, want := range []string{"Campaigns", "alpha", "active", "obey"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	m.width, m.height = 40, 10
	if m.View() == "" {
		t.Error("constrained view should not be empty")
	}
}

func TestListTUI_Go_Enabled(t *testing.T) {
	m := newTestListModel(t) // cursor 0 = alpha (/tmp/a)
	m.gotoEnabled = true
	m = lkey(m, "g")
	if !m.quitting {
		t.Error("g should quit the browser")
	}
	if m.gotoPath != "/tmp/a" {
		t.Errorf("gotoPath = %q, want /tmp/a", m.gotoPath)
	}
}

func TestListTUI_Go_EnterAlsoGoes(t *testing.T) {
	m := newTestListModel(t)
	m.gotoEnabled = true
	m = lkey(m, "enter")
	if !m.quitting || m.gotoPath != "/tmp/a" {
		t.Errorf("enter should go: quitting=%v gotoPath=%q", m.quitting, m.gotoPath)
	}
}

func TestListTUI_Go_DisabledShowsHint(t *testing.T) {
	m := newTestListModel(t) // gotoEnabled defaults false
	m = lkey(m, "g")
	if m.quitting {
		t.Error("g must not quit when goto is disabled")
	}
	if m.gotoPath != "" {
		t.Errorf("gotoPath = %q, want empty when disabled", m.gotoPath)
	}
	if !m.statusErr || m.status == "" {
		t.Error("expected a shell-integration hint")
	}
}

func TestListTUI_EnterInOverlayMovesNotGoes(t *testing.T) {
	m := newTestListModel(t)
	m.gotoEnabled = true
	m = lkey(m, "m")
	m = lkey(m, "newco")
	m = lkey(m, "enter") // confirms the move, not a go
	if m.quitting {
		t.Error("enter inside the move overlay should not quit")
	}
	if got := campaignOrg(t, "A-1"); got != "newco" {
		t.Errorf("alpha org = %q, want newco (overlay enter moves)", got)
	}
}

func TestWriteGotoSelection(t *testing.T) {
	dir := t.TempDir()

	out := filepath.Join(dir, "sel")
	if err := writeGotoSelection(listTUIModel{gotoPath: "/abs/campaign"}, out); err != nil {
		t.Fatalf("write: %v", err)
	}
	if b, _ := os.ReadFile(out); string(b) != "/abs/campaign" {
		t.Errorf("wrote %q, want /abs/campaign", b)
	}

	out2 := filepath.Join(dir, "sel2")
	if err := writeGotoSelection(listTUIModel{}, out2); err != nil {
		t.Fatalf("write (no goto): %v", err)
	}
	if _, err := os.Stat(out2); !os.IsNotExist(err) {
		t.Error("file must not be written when no campaign was chosen")
	}

	if err := writeGotoSelection(listTUIModel{gotoPath: "/x"}, ""); err != nil {
		t.Errorf("no path-output should be a no-op, got %v", err)
	}
}
