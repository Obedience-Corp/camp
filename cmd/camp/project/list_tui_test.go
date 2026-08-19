package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/uitest"
)

func projFixture() []projectsvc.Project {
	return []projectsvc.Project{
		{Name: "web", Path: "projects/web", Type: projectsvc.TypeTypeScript, Source: projectsvc.SourceLinked, URL: "git@ex/web.git"},
		{Name: "fest", Path: "projects/fest", Type: projectsvc.TypeGo, Source: projectsvc.SourceSubmodule},
		{Name: "camp", Path: "projects/camp", Type: projectsvc.TypeGo, Source: projectsvc.SourceSubmodule, URL: "git@ex/camp.git"},
		{Name: "scripts", Path: "projects/scripts", Type: projectsvc.TypePython, Source: projectsvc.SourceCampaign},
		{Name: "notes", Path: "projects/notes", Type: "", Source: projectsvc.SourceCampaign},
	}
}

func newTestProjModel() projListModel {
	return newProjListModel("/tmp/campaign-root", projFixture())
}

func projKey(m projListModel, s string) projListModel {
	var msg tea.KeyMsg
	switch s {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	next, _ := m.Update(msg)
	return next.(projListModel)
}

func projSized(m projListModel, w, h int) projListModel {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(projListModel)
}

func visibleNames(m projListModel) []string {
	out := make([]string, len(m.visible))
	for i, e := range m.visible {
		out[i] = e.Name
	}
	return out
}

func TestProjModel_SortsByTypeThenName(t *testing.T) {
	m := newTestProjModel()
	got := strings.Join(visibleNames(m), ",")
	want := "camp,fest,web,scripts,notes"
	if got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}
}

func TestProjModel_GroupCycle(t *testing.T) {
	m := newTestProjModel()
	if m.groupBy != groupByType {
		t.Fatalf("default group = %s, want type", m.groupBy.label())
	}
	m = projKey(m, "f")
	if m.groupBy != groupBySource {
		t.Fatalf("after f = %s, want source", m.groupBy.label())
	}
	got := strings.Join(visibleNames(m), ",")
	want := "camp,fest,notes,scripts,web"
	if got != want {
		t.Fatalf("source order = %s, want %s", got, want)
	}
	m = projKey(m, "f")
	if m.groupBy != groupByNone {
		t.Fatalf("after second f = %s, want flat", m.groupBy.label())
	}
	got = strings.Join(visibleNames(m), ",")
	want = "camp,fest,notes,scripts,web"
	if got != want {
		t.Fatalf("flat order = %s, want %s", got, want)
	}
	m = projKey(m, "f")
	if m.groupBy != groupByType {
		t.Fatalf("cycle should wrap to type, got %s", m.groupBy.label())
	}
}

func TestProjModel_SearchFilter(t *testing.T) {
	m := newTestProjModel()
	m = projKey(m, "/")
	if m.overlay != projOverlaySearch {
		t.Fatal("slash should open search")
	}
	for _, r := range "scripts" {
		m = projKey(m, string(r))
	}
	if len(m.visible) != 1 || m.visible[0].Name != "scripts" {
		t.Fatalf("search scripts = %v", visibleNames(m))
	}
	m = projKey(m, "enter")
	if m.overlay != projOverlayNone {
		t.Fatal("enter should leave search")
	}
	if m.query != "scripts" {
		t.Fatalf("query = %q, want scripts", m.query)
	}
	if m.quitting {
		t.Fatal("enter without shell integration must not jump")
	}
	if !m.statusErr || m.status == "" {
		t.Fatal("enter without shell integration should hint how to enable go")
	}
	m = newTestProjModel()
	m = projKey(m, "/")
	for _, r := range "scripts" {
		m = projKey(m, string(r))
	}
	m = projKey(m, "esc")
	if m.query != "" || len(m.visible) != 5 {
		t.Fatalf("esc should clear search, query=%q visible=%v", m.query, visibleNames(m))
	}
}

func TestProjModel_SearchThenGo(t *testing.T) {
	m := newTestProjModel()
	m.gotoEnabled = true
	m = projKey(m, "/")
	for _, r := range "scripts" {
		m = projKey(m, string(r))
	}
	m = projKey(m, "enter")
	if !m.quitting {
		t.Fatal("enter in search should jump when shell integration is on")
	}
	want := filepath.Join("/tmp/campaign-root", "projects", "scripts")
	if m.gotoPath != want {
		t.Fatalf("gotoPath = %q, want %q", m.gotoPath, want)
	}
}

func TestProjModel_SearchMoveWithJK(t *testing.T) {
	m := newTestProjModel()
	m = projKey(m, "/")
	for _, r := range "go" {
		m = projKey(m, string(r))
	}
	if m.overlay != projOverlaySearch {
		t.Fatal("typing the filter must keep the search overlay open (g must not jump)")
	}
	if m.query != "go" {
		t.Fatalf("query = %q, want go (g must be typeable in search)", m.query)
	}
	got := visibleNames(m)
	if len(got) < 2 {
		t.Fatalf("expected multiple go matches, got %v", got)
	}
	for _, name := range got {
		if name != "camp" && name != "fest" {
			t.Fatalf("filtered visible = %v, want only Go projects (camp, fest)", got)
		}
	}
	m = projKey(m, "j")
	if m.overlay != projOverlaySearch {
		t.Fatal("j in search should move among filtered rows, not leave search")
	}
	if m.cursor != 1 {
		t.Fatalf("j in search should move, cursor=%d", m.cursor)
	}
	m = projKey(m, "k")
	if m.cursor != 0 {
		t.Fatalf("k in search should move back, cursor=%d", m.cursor)
	}
}

func TestProjModel_SearchTypesLetterG(t *testing.T) {
	m := newTestProjModel()
	m = projKey(m, "/")
	m = projKey(m, "g")
	if m.overlay != projOverlaySearch {
		t.Fatalf("g in search must type into the filter, overlay=%v query=%q status=%q", m.overlay, m.query, m.status)
	}
	if m.query != "g" {
		t.Fatalf("query = %q, want g", m.query)
	}
	if m.quitting {
		t.Fatal("g in search must not jump/quit")
	}
}

func TestProjModel_NavigationWraps(t *testing.T) {
	m := newTestProjModel()
	m = projKey(m, "k")
	if m.cursor != len(m.visible)-1 {
		t.Fatalf("up from top should wrap to %d, got %d", len(m.visible)-1, m.cursor)
	}
	m = projKey(m, "j")
	if m.cursor != 0 {
		t.Fatalf("down from bottom should wrap to 0, got %d", m.cursor)
	}
}

func TestProjModel_CopyPath(t *testing.T) {
	prev := ui.WriteClipboard
	t.Cleanup(func() { ui.WriteClipboard = prev })
	var copied string
	ui.WriteClipboard = func(s string) error { copied = s; return nil }

	m := newTestProjModel()
	m = projKey(m, "y")
	want := filepath.Join("/tmp/campaign-root", "projects", "camp")
	if copied != want {
		t.Fatalf("copied = %q, want %q", copied, want)
	}
	if m.status != "copied!" {
		t.Fatalf("status = %q, want copied!", m.status)
	}
}

func TestProjModel_GoNeedsShellIntegration(t *testing.T) {
	m := newTestProjModel()
	m = projKey(m, "enter")
	if m.quitting {
		t.Fatal("go without shell integration must not quit")
	}
	if m.gotoPath != "" {
		t.Fatalf("go without shell integration must not set a path, got %q", m.gotoPath)
	}
	if !m.statusErr || m.status == "" {
		t.Fatalf("go without shell integration should surface an error status, got %q", m.status)
	}
}

func TestProjModel_GoWithShellIntegration(t *testing.T) {
	m := newTestProjModel()
	m.gotoEnabled = true
	m.cursor = 2 // web
	m = projKey(m, "enter")
	if !m.quitting {
		t.Fatal("go with shell integration should quit")
	}
	want := filepath.Join("/tmp/campaign-root", "projects", "web")
	if m.gotoPath != want {
		t.Fatalf("gotoPath = %q, want %q", m.gotoPath, want)
	}
}

func TestProjModel_QuitKeys(t *testing.T) {
	for _, k := range []string{"q", "esc"} {
		m := projKey(newTestProjModel(), k)
		if !m.quitting {
			t.Fatalf("%q should quit", k)
		}
	}
}

func TestWriteGotoSelection(t *testing.T) {
	m := newTestProjModel()
	m.gotoPath = "/tmp/campaign-root/projects/fest"
	path := filepath.Join(t.TempDir(), "out")
	if err := writeGotoSelection(m, path); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "/tmp/campaign-root/projects/fest" {
		t.Fatalf("path-output = %q", data)
	}
	if err := writeGotoSelection(m, ""); err != nil {
		t.Errorf("empty path-output should be a no-op, got %v", err)
	}
}

func TestProjView_BoundedAtEverySize(t *testing.T) {
	sizes := []struct{ w, h int }{
		{120, 40}, {80, 24}, {60, 20}, {40, 10}, {30, 8}, {24, 6}, {20, 5}, {15, 6},
		{10, 3}, {8, 2}, {1, 1}, {0, 0}, {40, 1}, {1, 40},
	}
	for _, s := range sizes {
		m := projSized(newTestProjModel(), s.w, s.h)
		m.cursor = 2
		uitest.AssertBounded(t, m.View(), s.w, s.h)
	}
}

func TestProjView_SelectionVisibleWhenScrolled(t *testing.T) {
	m := projSized(newTestProjModel(), 40, 6)
	m.cursor = len(m.visible) - 1
	out := m.View()
	uitest.AssertBounded(t, out, 40, 6)
	if !strings.Contains(out, "> ") {
		t.Fatalf("selected cursor not rendered when scrolled:\n%s", out)
	}
}

func TestProjView_ScrolledKeepsGroupHeader(t *testing.T) {
	projects := make([]projectsvc.Project, 0, 20)
	for i := 0; i < 20; i++ {
		name := "svc-" + string(rune('a'+i))
		projects = append(projects, projectsvc.Project{
			Name: name, Path: "projects/" + name, Type: projectsvc.TypeGo, Source: projectsvc.SourceSubmodule,
		})
	}
	m := projSized(newProjListModel("/tmp/campaign-root", projects), 80, 12)
	out := m.View()
	uitest.AssertBounded(t, out, 80, 12)
	if !strings.Contains(out, "[") {
		t.Fatalf("expected a scrolled window, got:\n%s", out)
	}
	if !strings.Contains(out, "Go") {
		t.Fatalf("scrolled type grouping should keep a sticky group header:\n%s", out)
	}
}

func TestProjView_HasNoPhantomTrailingRow(t *testing.T) {
	m := projSized(newTestProjModel(), 80, 24)
	out := m.View()
	if strings.HasSuffix(out, "\n") {
		t.Fatalf("full-screen view ends with a phantom row: %q", out)
	}
}

func TestProjView_RendersTitleAndTypeGroups(t *testing.T) {
	m := projSized(newTestProjModel(), 80, 24)
	out := m.View()
	for _, want := range []string{"Projects", "Go", "TypeScript", "camp", "web"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q:\n%s", want, out)
		}
	}
}

func projTestCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("format", "table", "")
	c.Flags().BoolP("interactive", "i", false, "")
	return c
}

func TestProjectListTUIRequested(t *testing.T) {
	prevJSON, prevCount := projectListJSON, projectListCount
	t.Cleanup(func() { projectListJSON, projectListCount = prevJSON, prevCount })

	t.Run("bare TTY opens browser", func(t *testing.T) {
		projectListJSON, projectListCount = false, false
		if !projectListTUIRequested(projTestCmd(), true) {
			t.Fatal("bare command in a TTY should open the browser")
		}
	})
	t.Run("no TTY prints table", func(t *testing.T) {
		projectListJSON, projectListCount = false, false
		if projectListTUIRequested(projTestCmd(), false) {
			t.Fatal("non-terminal stdout should print the table")
		}
	})
	t.Run("json never opens browser", func(t *testing.T) {
		projectListJSON, projectListCount = true, false
		if projectListTUIRequested(projTestCmd(), true) {
			t.Fatal("--json should never open the browser")
		}
	})
	t.Run("count never opens browser", func(t *testing.T) {
		projectListJSON, projectListCount = false, true
		if projectListTUIRequested(projTestCmd(), true) {
			t.Fatal("--count should never open the browser")
		}
	})
	t.Run("non-table format prints table", func(t *testing.T) {
		projectListJSON, projectListCount = false, false
		c := projTestCmd()
		_ = c.Flags().Set("format", "simple")
		if projectListTUIRequested(c, true) {
			t.Fatal("--format simple should print the table")
		}
	})
	t.Run("interactive forces browser", func(t *testing.T) {
		projectListJSON, projectListCount = false, false
		c := projTestCmd()
		_ = c.Flags().Set("interactive", "true")
		if !projectListTUIRequested(c, false) {
			t.Fatal("-i should force the browser even without a TTY")
		}
	})
}

func TestProjectListInteractiveFlagsRegistered(t *testing.T) {
	if projectListCmd.Flags().Lookup("interactive") == nil {
		t.Fatal("camp project list missing --interactive flag")
	}
	if projectListCmd.Flags().Lookup("path-output") == nil {
		t.Fatal("camp project list missing --path-output flag")
	}
}
