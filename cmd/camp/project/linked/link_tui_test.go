package linked

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	projectsvc "github.com/Obedience-Corp/camp/internal/project"
)

func linkFixtureDirs(path string, _ bool) ([]linkEntry, error) {
	switch path {
	case "/home":
		return []linkEntry{
			{label: "campaign", path: "/home/campaign", kind: "dir", badge: "git"},
			{label: "src", path: "/home/src", kind: "dir"},
		}, nil
	case "/home/src":
		return []linkEntry{
			{label: "ledger", path: "/home/src/ledger", kind: "dir", badge: "go"},
			{label: "notes", path: "/home/src/notes", kind: "dir"},
		}, nil
	default:
		return nil, nil
	}
}

func linkFixtureCamps() []linkCamp {
	return []linkCamp{
		{ID: "b", Name: "bravo", Path: "/camps/bravo"},
		{ID: "a", Name: "alpha", Path: "/camps/alpha"},
	}
}

func linkOpenBrowse() linkOpen {
	return linkOpen{
		ctx:       context.Background(),
		browse:    "/home",
		offerHere: false,
		camps:     linkFixtureCamps(),
		camp:      linkCamp{ID: "a", Name: "alpha", Path: "/camps/alpha"},
		hasCamp:   true,
		homes:     []string{"/home"},
		listFn:    linkFixtureDirs,
	}
}

func linkKey(m linkModel, key string) (linkModel, tea.Cmd) {
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
	case "ctrl+c":
		msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, cmd := m.Update(msg)
	return next.(linkModel), cmd
}

func TestLinkBrowse_OffersTheCurrentFolder(t *testing.T) {
	open := linkOpenBrowse()
	open.browse = "/home/src/ledger"
	open.offerHere = true
	m := newLinkModel(open)
	if m.step != stepFolder {
		t.Fatalf("step = %d, want browse", m.step)
	}
	entry, ok := m.selected()
	if !ok || entry.kind != "here" {
		t.Fatalf("selected = %+v", entry)
	}
	m, _ = linkKey(m, "enter")
	if m.step != stepName || m.chosenPath != "/home/src/ledger" {
		t.Fatalf("path %q step %d", m.chosenPath, m.step)
	}
	if m.nameInput.Value() != "ledger" {
		t.Fatalf("name = %q", m.nameInput.Value())
	}
}

func TestLinkBrowse_LLinksTheHighlightedProject(t *testing.T) {
	m := newLinkModel(linkOpenBrowse())
	// Home lists campaign, then src. Move onto src and open it.
	m, _ = linkKey(m, "down")
	m, _ = linkKey(m, "enter")
	if m.cwd != "/home/src" {
		t.Fatalf("cwd = %q", m.cwd)
	}
	entry, _ := m.selected()
	if entry.label != "ledger" {
		t.Fatalf("selected = %+v", entry)
	}
	m, _ = linkKey(m, "l")
	if m.step != stepName || m.chosenPath != "/home/src/ledger" || m.nameInput.Value() != "ledger" {
		t.Fatalf("linked path %q name %q step %d", m.chosenPath, m.nameInput.Value(), m.step)
	}
}

func TestLinkBrowse_FilterNarrowsFolders(t *testing.T) {
	open := linkOpenBrowse()
	open.browse = "/home/src"
	m := newLinkModel(open)
	m, _ = linkKey(m, "/")
	m, _ = linkKey(m, "n")
	if m.hasDir() != true {
		t.Fatal("expected a match")
	}
	found := false
	for _, entry := range m.visible {
		if entry.kind == "dir" && entry.label == "notes" {
			found = true
		}
		if entry.label == "ledger" {
			t.Fatal("ledger should be filtered out")
		}
	}
	if !found {
		t.Fatalf("visible = %+v", m.visible)
	}
}

func TestLinkBrowse_JumpToAPath(t *testing.T) {
	open := linkOpenBrowse()
	open.statFn = func(path string) (bool, error) {
		if path == "/srv/app" {
			return true, nil
		}
		return false, errors.New("missing")
	}
	m := newLinkModel(open)
	m, _ = linkKey(m, "g")
	for _, r := range "/srv/app" {
		m, _ = linkKey(m, string(r))
	}
	m, _ = linkKey(m, "enter")
	if m.cwd != "/srv/app" || m.step != stepFolder {
		t.Fatalf("cwd %q step %d err %q", m.cwd, m.step, m.errMsg)
	}
	entry, _ := m.selected()
	if entry.kind != "here" {
		t.Fatalf("after jump selected = %+v", entry)
	}
}

func TestLinkBrowse_HiddenToggleReloads(t *testing.T) {
	hidden := false
	open := linkOpenBrowse()
	open.listFn = func(path string, showHidden bool) ([]linkEntry, error) {
		hidden = showHidden
		return linkFixtureDirs(path, showHidden)
	}
	m := newLinkModel(open)
	if hidden {
		t.Fatal("first load should hide dot dirs")
	}
	m, _ = linkKey(m, ".")
	if !hidden || !m.showHidden {
		t.Fatalf("hidden = %v", m.showHidden)
	}
}

func TestLinkName_EmptyStaysPut(t *testing.T) {
	open := linkOpenBrowse()
	open.browse = ""
	open.chosen = "/home/src/ledger"
	m := newLinkModel(open)
	if m.step != stepName {
		t.Fatalf("step = %d, want name", m.step)
	}
	m.nameInput.SetValue("")
	m, _ = linkKey(m, "enter")
	if m.step != stepName || m.errMsg == "" {
		t.Fatalf("step %d err %q", m.step, m.errMsg)
	}
}

func TestLinkName_PresetEscQuitsWithoutApply(t *testing.T) {
	applied := 0
	open := linkOpenBrowse()
	open.browse = ""
	open.chosen = "/home/src/ledger"
	open.applyFn = func(context.Context, linkCamp, *projectsvc.LinkPlan, bool) (*projectsvc.LinkResult, linkCommitNote, error) {
		applied++
		return nil, linkCommitNote{}, nil
	}
	m := newLinkModel(open)
	m, cmd := linkKey(m, "esc")
	if !m.quitting || cmd == nil || applied != 0 {
		t.Fatalf("quitting=%v cmd=%v applied=%d", m.quitting, cmd != nil, applied)
	}
}

func TestLinkName_AsksForACampWhenNoneIsSelected(t *testing.T) {
	open := linkOpen{
		ctx:    context.Background(),
		chosen: "/home/src/ledger",
		camps:  linkFixtureCamps(),
		homes:  []string{"/home"},
		listFn: linkFixtureDirs,
		planFn: func(context.Context, string, string, string) (*projectsvc.LinkPlan, error) {
			return nil, errors.New("should not plan yet")
		},
	}
	m := newLinkModel(open)
	m, _ = linkKey(m, "enter")
	if m.step != stepCamp {
		t.Fatalf("step = %d, want camp", m.step)
	}
	view := m.View()
	if !strings.Contains(view, "Choose a camp") || !strings.Contains(view, "alpha") || !strings.Contains(view, "bravo") {
		t.Fatalf("camp view:\n%s", view)
	}
	// alpha sorts first. Move to bravo and choose it.
	m, _ = linkKey(m, "down")
	m, cmd := linkKey(m, "enter")
	if m.step != stepReview || !m.planning || m.camp.Name != "bravo" || cmd == nil {
		t.Fatalf("camp %q step %d planning %v", m.camp.Name, m.step, m.planning)
	}
}

func TestLinkReview_QuitDoesNotApply(t *testing.T) {
	applied := 0
	open := linkOpenBrowse()
	open.browse = ""
	open.chosen = "/home/src/ledger"
	open.name = "ledger"
	open.nameSet = true
	open.applyFn = func(context.Context, linkCamp, *projectsvc.LinkPlan, bool) (*projectsvc.LinkResult, linkCommitNote, error) {
		applied++
		return nil, linkCommitNote{}, nil
	}
	m := newLinkModel(open)
	m.planning = false
	m.plan = &projectsvc.LinkPlan{Name: "ledger", Path: "projects/ledger", Source: "/home/src/ledger", IsGit: true, Type: "go"}
	m, cmd := linkKey(m, "q")
	if !m.quitting || cmd == nil || applied != 0 {
		t.Fatalf("quitting=%v cmd=%v applied=%d", m.quitting, cmd != nil, applied)
	}
}

func TestLinkReview_ConfirmAppliesAndDoneShowsUndo(t *testing.T) {
	open := linkOpenBrowse()
	open.browse = ""
	open.chosen = "/home/src/ledger"
	open.name = "ledger"
	open.nameSet = true
	open.applyFn = func(context.Context, linkCamp, *projectsvc.LinkPlan, bool) (*projectsvc.LinkResult, linkCommitNote, error) {
		return &projectsvc.LinkResult{Name: "ledger", Path: "projects/ledger", Source: "/home/src/ledger", IsGit: true, Type: "go"}, linkCommitNote{Committed: true, Message: "Committed changes to git"}, nil
	}
	m := newLinkModel(open)
	m.planning = false
	m.plan = &projectsvc.LinkPlan{
		Name: "ledger", Path: "projects/ledger", Source: "/home/src/ledger",
		IsGit: true, Type: "go", CampaignName: "alpha",
	}
	m.width, m.height = 100, 36
	confirmed, _ := m.confirm()
	m = confirmed.(linkModel)
	if m.step != stepWork {
		t.Fatalf("step = %d, want working", m.step)
	}
	msg := m.applyCmd()().(linkAppliedMsg)
	applied, _ := m.applied(msg)
	m = applied.(linkModel)
	if m.step != stepDone {
		t.Fatalf("step = %d, want done", m.step)
	}
	view := m.View()
	for _, needle := range []string{"Project linked", "not a git submodule", "projects/ledger", "camp project unlink ledger", "committed"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("view missing %q\n%s", needle, view)
		}
	}
}

func TestLinkReview_PlanErrorReturnsToName(t *testing.T) {
	open := linkOpenBrowse()
	open.browse = ""
	open.chosen = "/home/src/ledger"
	open.planFn = func(context.Context, string, string, string) (*projectsvc.LinkPlan, error) {
		return nil, errors.New("project already exists")
	}
	m := newLinkModel(open)
	m, cmd := linkKey(m, "enter")
	if m.step != stepReview || !m.planning || cmd == nil {
		t.Fatalf("step %d planning %v", m.step, m.planning)
	}
	planned := cmd().(linkPlannedMsg)
	named, _ := m.planned(planned)
	m = named.(linkModel)
	if m.step != stepName || !strings.Contains(m.errMsg, "already exists") {
		t.Fatalf("step %d err %q", m.step, m.errMsg)
	}
}

func TestLinkReview_ViewShowsTheShortcutBeforeWriting(t *testing.T) {
	open := linkOpenBrowse()
	open.browse = ""
	open.chosen = "/home/src/ledger"
	open.name = "ledger"
	open.nameSet = true
	m := newLinkModel(open)
	m.planning = false
	m.plan = &projectsvc.LinkPlan{
		Name: "ledger", Path: "projects/ledger", Source: "/home/src/ledger",
		IsGit: true, Type: "go", CampaignName: "alpha",
	}
	m.width, m.height = 100, 36
	view := m.View()
	for _, needle := range []string{"not a git submodule", "Nothing is written until you confirm.", "projects/ledger", "enter link", "yes, go", "~/src/ledger"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("view missing %q\n%s", needle, view)
		}
	}
}

func TestLinkReview_WarnsWhenTheFolderIsInsideTheCamp(t *testing.T) {
	m := newLinkModel(linkOpen{
		ctx:     context.Background(),
		chosen:  "/camp/notes",
		name:    "notes",
		nameSet: true,
		hasCamp: true,
		camp:    linkCamp{ID: "c", Name: "demo", Path: "/camp"},
		camps:   []linkCamp{{ID: "c", Name: "demo", Path: "/camp"}},
		listFn:  linkFixtureDirs,
	})
	m.planning = false
	m.plan = &projectsvc.LinkPlan{Name: "notes", Path: "projects/notes", Source: "/camp/notes", CampaignName: "demo"}
	m.width, m.height = 100, 36
	if !strings.Contains(m.View(), "already inside the camp") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestLinkUsesTUI(t *testing.T) {
	if linkUsesTUI(linkFlags{}, false) {
		t.Fatal("piped link should write immediately")
	}
	if !linkUsesTUI(linkFlags{}, true) {
		t.Fatal("a terminal should open the browser")
	}
	if linkUsesTUI(linkFlags{yes: true}, true) {
		t.Fatal("--yes should write immediately")
	}
	if !linkUsesTUI(linkFlags{interactive: true}, false) {
		t.Fatal("-i should request the browser even before the tty check")
	}
	if linkUsesTUI(linkFlags{yes: true, interactive: true}, true) {
		t.Fatal("--yes wins over -i")
	}
}

func TestLinkBrowseFrom(t *testing.T) {
	got, offer := linkBrowseFrom("/work/app", "/camp", "/home", false)
	if got != "/work/app" || !offer {
		t.Fatalf("outside = %q offer %v", got, offer)
	}
	got, offer = linkBrowseFrom("/camp/projects", "/camp", "/home", true)
	if got != "/home" || offer {
		t.Fatalf("inside = %q offer %v", got, offer)
	}
	got, offer = linkBrowseFrom("/camp", "/camp", "", true)
	if got != "/" || offer {
		t.Fatalf("inside without home = %q offer %v", got, offer)
	}
}

func TestLinkInsideCamp(t *testing.T) {
	if !linkInsideCamp("/camp/projects/web", "/camp") {
		t.Fatal("a project inside the camp should count as inside")
	}
	if linkInsideCamp("/work/app", "/camp") {
		t.Fatal("an outside folder should not count as inside")
	}
	if linkInsideCamp("/work/app", "") {
		t.Fatal("missing camp root")
	}
}
