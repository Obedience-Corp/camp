package project

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	projectrename "github.com/Obedience-Corp/camp/internal/project/rename"
)

func renameFixtureProjects() []projectsvc.Project {
	return []projectsvc.Project{
		{Name: "web", Path: "projects/web", Type: projectsvc.TypeTypeScript, Source: projectsvc.SourceLinked},
		{Name: "atlas", Path: "projects/atlas", Type: projectsvc.TypeGo, Source: projectsvc.SourceCampaign},
		{Name: "notes", Path: "projects/notes", Type: "", Source: projectsvc.SourceCampaign},
	}
}

func renameFixturePlan() *projectrename.PlanResult {
	return &projectrename.PlanResult{
		Kind:               projectrename.KindCampaignDir,
		OldName:            "atlas",
		NewName:            "atlas-core",
		OldPath:            "projects/atlas",
		NewPath:            "projects/atlas-core",
		AutoCommitEligible: true,
	}
}

func renameKey(m renameModel, key string) (renameModel, tea.Cmd) {
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, cmd := m.Update(msg)
	return next.(renameModel), cmd
}

func TestRenameModel_SortsAndSelects(t *testing.T) {
	m := newRenameModel(t.Context(), "/camp", "", renameFixtureProjects(), nil, projectRenameFlags{})
	if m.step != renamePick {
		t.Fatalf("step = %d, want pick", m.step)
	}
	got := []string{m.visible[0].Name, m.visible[1].Name, m.visible[2].Name}
	want := []string{"atlas", "notes", "web"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
	m, _ = renameKey(m, "down")
	m, _ = renameKey(m, "enter")
	if m.step != renameName || m.oldName != "notes" {
		t.Fatalf("chose %q at step %d", m.oldName, m.step)
	}
}

func TestRenameModel_FilterNarrowsTheList(t *testing.T) {
	m := newRenameModel(t.Context(), "/camp", "", renameFixtureProjects(), nil, projectRenameFlags{})
	m, _ = renameKey(m, "/")
	m, _ = renameKey(m, "w")
	if len(m.visible) != 1 || m.visible[0].Name != "web" {
		t.Fatalf("visible = %+v", m.visible)
	}
	m, _ = renameKey(m, "enter")
	if m.oldName != "web" || m.step != renameName {
		t.Fatalf("filter choose = %q step %d", m.oldName, m.step)
	}
}

func TestRenameModel_EmptyNameStaysPut(t *testing.T) {
	m := newRenameModel(t.Context(), "/camp", "", renameFixtureProjects(), []string{"atlas"}, projectRenameFlags{})
	if m.step != renameName {
		t.Fatalf("step = %d, want name", m.step)
	}
	m, _ = renameKey(m, "enter")
	if m.step != renameName || m.errMsg == "" {
		t.Fatalf("step %d err %q", m.step, m.errMsg)
	}
}

func TestRenameModel_ReviewQuitDoesNotApply(t *testing.T) {
	m := newRenameModel(t.Context(), "/camp", "", renameFixtureProjects(), []string{"atlas", "atlas-core"}, projectRenameFlags{})
	m.planning = false
	m.plan = renameFixturePlan()
	applied := 0
	m.applyFn = func(context.Context, string, string, *projectrename.PlanResult, string, projectRenameFlags) (*projectrename.Result, *projectRenameCommit, error) {
		applied++
		return nil, nil, nil
	}
	m, cmd := renameKey(m, "q")
	if !m.quitting || cmd == nil || applied != 0 {
		t.Fatalf("quitting=%v cmd=%v applied=%d", m.quitting, cmd != nil, applied)
	}
}

func TestRenameModel_ConfirmAppliesAndDoneShowsUndo(t *testing.T) {
	m := newRenameModel(t.Context(), "/camp", "", renameFixtureProjects(), []string{"atlas", "atlas-core"}, projectRenameFlags{})
	m.planning = false
	m.plan = renameFixturePlan()
	m.applyFn = func(context.Context, string, string, *projectrename.PlanResult, string, projectRenameFlags) (*projectrename.Result, *projectRenameCommit, error) {
		return &projectrename.Result{Plan: renameFixturePlan(), Verified: true}, &projectRenameCommit{Committed: true, Message: "Rename: atlas -> atlas-core"}, nil
	}
	confirmed, _ := m.confirm()
	m = confirmed.(renameModel)
	if m.step != renameWorking {
		t.Fatalf("step = %d, want working", m.step)
	}
	msg := m.applyCmd()().(renameAppliedMsg)
	appliedModel, _ := m.applied(msg)
	m = appliedModel.(renameModel)
	if m.step != renameDone {
		t.Fatalf("step = %d, want done", m.step)
	}
	view := m.View()
	for _, needle := range []string{"atlas-core", "committed", "camp project rename atlas-core atlas", "camp directory"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("view missing %q\n%s", needle, view)
		}
	}
}

func TestRenameModel_PlanErrorReturnsToName(t *testing.T) {
	m := newRenameModel(t.Context(), "/camp", "", renameFixtureProjects(), []string{"atlas"}, projectRenameFlags{})
	m.nameInput.SetValue("atlas-core")
	m.planFn = func(context.Context, string, string, string, projectrename.Options) (*projectrename.PlanResult, error) {
		return nil, errors.New("project destination already exists")
	}
	m, cmd := renameKey(m, "enter")
	if m.step != renameReview || !m.planning || cmd == nil {
		t.Fatalf("step %d planning %v", m.step, m.planning)
	}
	planned := cmd().(renamePlannedMsg)
	named, _ := m.planned(planned)
	m = named.(renameModel)
	if m.step != renameName || !strings.Contains(m.errMsg, "already exists") {
		t.Fatalf("step %d err %q", m.step, m.errMsg)
	}
}

func TestRenameModel_DryRunClosesWithoutApply(t *testing.T) {
	m := newRenameModel(t.Context(), "/camp", "", renameFixtureProjects(), []string{"atlas", "atlas-core"}, projectRenameFlags{dryRun: true})
	m.planning = false
	m.plan = renameFixturePlan()
	applied := 0
	m.applyFn = func(context.Context, string, string, *projectrename.PlanResult, string, projectRenameFlags) (*projectrename.Result, *projectRenameCommit, error) {
		applied++
		return nil, nil, errors.New("should not apply")
	}
	confirmed, _ := m.confirm()
	m = confirmed.(renameModel)
	if m.step != renameDone || applied != 0 {
		t.Fatalf("step %d applied %d", m.step, applied)
	}
	if !strings.Contains(m.View(), "No changes made.") {
		t.Fatalf("dry-run view:\n%s", m.View())
	}
}

func TestRenameReviewViewShowsConfirm(t *testing.T) {
	m := newRenameModel(t.Context(), "/camp", "", renameFixtureProjects(), nil, projectRenameFlags{})
	m.width, m.height = 100, 36
	m.step = renameReview
	m.oldName = "atlas"
	m.newName = "atlas-core"
	m.plan = renameFixturePlan()
	view := m.View()
	for _, needle := range []string{"atlas", "atlas-core", "enter rename", "camp directory", "projects/atlas"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("view missing %q\n%s", needle, view)
		}
	}
}

func TestProjectRenameArgsFollowTheTerminal(t *testing.T) {
	prev := stdoutIsTTY
	t.Cleanup(func() { stdoutIsTTY = prev })

	stdoutIsTTY = func() bool { return false }
	cmd := newProjectRenameCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Fatal("piped rename should require both names")
	}
	if err := cmd.Args(cmd, []string{"old", "new"}); err != nil {
		t.Fatal(err)
	}

	stdoutIsTTY = func() bool { return true }
	cmd = newProjectRenameCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Args(cmd, []string{}); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Fatal("--json should require both names")
	}
	if err := cmd.Flags().Set("json", "false"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Fatal("--yes should require both names")
	}
}
