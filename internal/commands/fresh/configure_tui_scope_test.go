package fresh

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func globalAndProjectConfig() *config.FreshConfig {
	return &config.FreshConfig{
		FollowUp: []config.FollowUpConfig{
			{Name: "install", Run: "npm install"},
			{Name: "build", Run: "npm run build"},
		},
		Projects: map[string]config.FreshProjectConfig{
			"api": {FollowUp: []config.FollowUpConfig{{Name: "gen", Run: "just gen"}}},
		},
	}
}

func TestSelectProjectScopeOpensOnDetectedProject(t *testing.T) {
	m := newFollowUpTUIModel(context.Background(), "/campaign", []project.Project{{Name: "api"}, {Name: "web"}}, globalAndProjectConfig())

	if got := m.selectedScope(); got != globalFollowUpScope {
		t.Fatalf("default scope = %q, want global", got)
	}

	m.selectProjectScope("web")

	if got := m.selectedScope(); got != "web" {
		t.Fatalf("selected scope = %q, want web", got)
	}
	if !strings.Contains(m.status, "project web") {
		t.Errorf("status %q does not name the detected project", m.status)
	}
	if m.statusLevel == statusError {
		t.Error("detecting a project scope reported an error status")
	}
}

// A worktree or linked repo need not appear in project.List, but fresh still
// resolves overrides under its name, so the scope has to be reachable.
func TestSelectProjectScopeAddsUnlistedProject(t *testing.T) {
	m := newFollowUpTUIModel(context.Background(), "/campaign", []project.Project{{Name: "api"}}, globalAndProjectConfig())

	m.selectProjectScope("web@feature")

	if got := m.selectedScope(); got != "web@feature" {
		t.Fatalf("selected scope = %q, want web@feature", got)
	}
}

func TestSelectProjectScopeIgnoresEmptyName(t *testing.T) {
	m := newFollowUpTUIModel(context.Background(), "/campaign", []project.Project{{Name: "api"}}, globalAndProjectConfig())

	m.selectProjectScope("")

	if got := m.selectedScope(); got != globalFollowUpScope {
		t.Fatalf("selected scope = %q, want global", got)
	}
	if m.status != "" {
		t.Errorf("status = %q, want empty when nothing was detected", m.status)
	}
}

func TestScopeInheritsGlobal(t *testing.T) {
	m := newFollowUpTUIModel(context.Background(), "/campaign", []project.Project{{Name: "api"}, {Name: "web"}}, globalAndProjectConfig())

	if m.scopeInheritsGlobal() {
		t.Error("the global scope must never report as inheriting")
	}

	m.rebuildScopes("web")
	if !m.scopeInheritsGlobal() {
		t.Error("web has no follow-up list and must report as inheriting")
	}

	m.rebuildScopes("api")
	if m.scopeInheritsGlobal() {
		t.Error("api has its own follow-up list and must not report as inheriting")
	}
}

func TestForkNoticeDescribesWhatSavingDoes(t *testing.T) {
	m := newFollowUpTUIModel(context.Background(), "/campaign", []project.Project{{Name: "api"}, {Name: "web"}}, globalAndProjectConfig())

	if got := m.forkNotice(); got != "" {
		t.Errorf("global scope notice = %q, want empty", got)
	}

	m.rebuildScopes("api")
	if got := m.forkNotice(); got != "" {
		t.Errorf("scope with its own list notice = %q, want empty", got)
	}

	m.rebuildScopes("web")
	want := "Saving copies the 2 global steps into this project's own list."
	if got := m.forkNotice(); got != want {
		t.Errorf("inheriting scope notice = %q, want %q", got, want)
	}
}

func TestForkNoticeWithNoGlobalSteps(t *testing.T) {
	cfg := &config.FreshConfig{}
	m := newFollowUpTUIModel(context.Background(), "/campaign", []project.Project{{Name: "web"}}, cfg)
	m.rebuildScopes("web")

	want := "Saving creates a follow-up list for this project."
	if got := m.forkNotice(); got != want {
		t.Errorf("notice = %q, want %q", got, want)
	}
}

// Adding a step whose name matches an inherited one used to be rejected with
// "appears more than once" from deep in config validation, which named neither
// the offending list nor the way out. The check now runs before the write and
// points at the edit that produces a project-specific version of the step.
func TestSaveFormRejectsInheritedNameWithActionableError(t *testing.T) {
	m := newFollowUpTUIModel(context.Background(), "/campaign", []project.Project{{Name: "web"}}, globalAndProjectConfig())
	m.rebuildScopes("web")
	m.openFollowUpForm(false, config.FollowUpConfig{})
	m.inputs[0].SetValue("build")
	m.inputs[1].SetValue("go build ./...")

	if cmd := m.saveForm(); cmd != nil {
		t.Fatal("saveForm returned a command for a rejected entry")
	}
	if !strings.Contains(m.formError, "inherited from global") {
		t.Errorf("formError = %q, want it to name the inherited source", m.formError)
	}
	if !strings.Contains(m.formError, "press e") {
		t.Errorf("formError = %q, want it to point at the edit path", m.formError)
	}
	if m.overlay != followUpFormOverlay {
		t.Error("the form closed on a rejected save")
	}
}

func TestSaveFormRejectsDuplicateNameInOwnScope(t *testing.T) {
	m := newFollowUpTUIModel(context.Background(), "/campaign", []project.Project{{Name: "api"}}, globalAndProjectConfig())
	m.rebuildScopes("api")
	m.openFollowUpForm(false, config.FollowUpConfig{})
	m.inputs[0].SetValue("gen")
	m.inputs[1].SetValue("just gen")

	if cmd := m.saveForm(); cmd != nil {
		t.Fatal("saveForm returned a command for a rejected entry")
	}
	if !strings.Contains(m.formError, "already exists here") {
		t.Errorf("formError = %q, want a same-scope duplicate message", m.formError)
	}
	if strings.Contains(m.formError, "inherited") {
		t.Errorf("formError = %q, must not blame global for a same-scope duplicate", m.formError)
	}
}

// Rows must stay one line: a wrapped row costs the pane a row it has not
// budgeted, which pushes its bottom border off the terminal.
func TestScopeRowTextFitsWidth(t *testing.T) {
	tests := []struct {
		name  string
		scope followUpScope
		width int
		want  string
	}{
		{
			name:  "plain project",
			scope: followUpScope{name: "camp"},
			width: 20,
			want:  "camp",
		},
		{
			name:  "badges when they fit",
			scope: followUpScope{name: "camp", current: true, override: true, overrideCount: 3},
			width: 40,
			want:  "camp · here · override 3",
		},
		{
			name:  "override badge kept when only one fits",
			scope: followUpScope{name: "webapp", current: true, override: true, overrideCount: 4},
			width: 25,
			want:  "webapp · override 4",
		},
		{
			name:  "here badge kept when it is the only one",
			scope: followUpScope{name: "webapp", current: true},
			width: 25,
			want:  "webapp · here",
		},
		{
			name:  "badges dropped before the name is cut",
			scope: followUpScope{name: "festival-app-design", current: true},
			width: 19,
			want:  "festival-app-design",
		},
		{
			name:  "name truncated when even it will not fit",
			scope: followUpScope{name: "festival-app-design", override: true, overrideCount: 2},
			width: 10,
			want:  "festiva...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scopeRowText(tt.scope, tt.width)
			if got != tt.want {
				t.Fatalf("scopeRowText() = %q, want %q", got, tt.want)
			}
			if w := lipgloss.Width(got); w > tt.width {
				t.Fatalf("scopeRowText() width %d exceeds %d: %q", w, tt.width, got)
			}
		})
	}
}

// The regression that shipped: long labels were clamped to the pane's block
// width, which lipgloss then wrapped because padding eats into it. Every
// rendered pane row must stay inside the box.
func TestRenderedPanesStayWithinTheirRowBudget(t *testing.T) {
	cfg := globalAndProjectConfig()
	projects := []project.Project{
		{Name: "festival-app-design"}, {Name: "obediencecorp.com"}, {Name: "api"},
		{Name: "web"}, {Name: "camp-scaffold"}, {Name: "festival-app@protos"},
	}
	m := newFollowUpTUIModel(context.Background(), "/campaign", projects, cfg)
	m.selectProjectScope("festival-app-design")

	for _, size := range [][2]int{{110, 30}, {90, 20}, {80, 14}, {160, 40}} {
		width, height := size[0], size[1]
		m.width, m.height = width, height
		lay := m.layout()

		for _, pane := range []struct {
			label  string
			render string
		}{
			{"scopes", m.renderScopesPane(lay)},
			{"workflow", m.renderWorkflowPane(lay)},
		} {
			lines := strings.Split(pane.render, "\n")
			// bodyRows content rows, plus the two border rows the box adds.
			if want := lay.bodyRows + 4; len(lines) != want {
				t.Errorf("%dx%d %s pane rendered %d rows, want %d", width, height, pane.label, len(lines), want)
			}
			for i, line := range lines {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("%dx%d %s pane row %d width %d exceeds terminal width: %q", width, height, pane.label, i, w, line)
				}
			}
		}
	}
}

// The dimmed backdrop behind an overlay has to cover the whole canvas. Setting
// it on a style wrapped around lipgloss.Place did not: the box emits its own
// resets, so the filler to the right of the box on the box's own rows came out
// unstyled and showed as a black notch down one side of the screen.
func TestOverlayBackdropCoversEveryRow(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := newFollowUpTUIModel(context.Background(), "/campaign", []project.Project{{Name: "payments-api"}}, globalAndProjectConfig())
	m.selectProjectScope("payments-api")
	m.width, m.height = 150, 35
	m.openFollowUpForm(false, config.FollowUpConfig{})

	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Fatalf("overlay rendered %d rows, want %d", len(lines), m.height)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w != m.width {
			t.Errorf("row %d width %d, want %d", i, w, m.width)
		}
		for _, col := range unpaintedMarginColumns(line) {
			t.Errorf("row %d has backdrop with no background at column %d: %q", i, col, line)
		}
	}
}

// unpaintedMarginColumns reports the columns in a rendered row's leading and
// trailing whitespace that carry no background color. A terminal draws those
// cells in its own background, so they are holes in the backdrop around the
// overlay box. Only the margins are checked: whitespace between the box
// borders belongs to the widgets inside it, which style themselves.
func unpaintedMarginColumns(line string) []int {
	type cell struct {
		r       rune
		painted bool
	}
	var cells []cell

	bg := false
	for i := 0; i < len(line); {
		if strings.HasPrefix(line[i:], "\x1b[") {
			end := strings.IndexByte(line[i:], 'm')
			if end < 0 {
				break
			}
			for _, param := range strings.Split(line[i+2:i+end], ";") {
				switch param {
				case "0", "":
					bg = false
				case "48":
					bg = true
				}
			}
			i += end + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		cells = append(cells, cell{r: r, painted: bg})
		i += size
	}

	var unpainted []int
	for i := 0; i < len(cells) && cells[i].r == ' '; i++ {
		if !cells[i].painted {
			unpainted = append(unpainted, i)
		}
	}
	for i := len(cells) - 1; i >= 0 && cells[i].r == ' '; i-- {
		if !cells[i].painted {
			unpainted = append(unpainted, i)
		}
	}
	return unpainted
}
