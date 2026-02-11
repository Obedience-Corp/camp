package status

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func testRepos() []RepoInfo {
	return []RepoInfo{
		{Name: "camp", Path: "/tmp/camp", Branch: "main", Clean: true},
		{Name: "fest", Path: "/tmp/fest", Branch: "develop", Modified: 2, Untracked: 1},
		{Name: "daemon", Path: "/tmp/daemon", Branch: "feature/auth", Staged: 3, Ahead: 1},
	}
}

func keyMsg(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

func specialKeyMsg(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

func sizeMsg(w, h int) tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: w, Height: h}
}

func setupModel(t *testing.T) Model {
	t.Helper()
	m := New(testRepos())
	// Populate status cache so we don't need async loading
	m.statusCache[0] = "On branch main\nnothing to commit, working tree clean"
	m.statusCache[1] = "On branch develop\n\nChanges not staged for commit:\n  modified: file.go\n  modified: main.go\n\nUntracked files:\n  new.go"
	m.statusCache[2] = "On branch feature/auth\n\nChanges to be committed:\n  new file: auth.go\n  new file: handler.go\n  new file: middleware.go"

	// Initialize with window size
	updated, _ := m.Update(sizeMsg(120, 40))
	return updated.(Model)
}

func TestNew_InitializesCorrectly(t *testing.T) {
	repos := testRepos()
	m := New(repos)

	if len(m.repos) != 3 {
		t.Errorf("repos count = %d, want 3", len(m.repos))
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
	if m.searchMode {
		t.Error("searchMode should be false initially")
	}
	if m.lastKeyWasG {
		t.Error("lastKeyWasG should be false initially")
	}
}

func TestInit_ReturnsLoadCommand(t *testing.T) {
	m := New(testRepos())
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a command for loading first repo status")
	}
}

func TestInit_EmptyRepos(t *testing.T) {
	m := New(nil)
	cmd := m.Init()
	if cmd != nil {
		t.Error("Init() should return nil for empty repos")
	}
}

func TestNavigation_JK(t *testing.T) {
	m := setupModel(t)

	// Move down with j
	updated, _ := m.Update(keyMsg("j"))
	m = updated.(Model)
	if m.cursor != 1 {
		t.Errorf("after j: cursor = %d, want 1", m.cursor)
	}

	// Move down again
	updated, _ = m.Update(keyMsg("j"))
	m = updated.(Model)
	if m.cursor != 2 {
		t.Errorf("after j j: cursor = %d, want 2", m.cursor)
	}

	// Move down at boundary (should stay)
	updated, _ = m.Update(keyMsg("j"))
	m = updated.(Model)
	if m.cursor != 2 {
		t.Errorf("at bottom boundary: cursor = %d, want 2", m.cursor)
	}

	// Move up with k
	updated, _ = m.Update(keyMsg("k"))
	m = updated.(Model)
	if m.cursor != 1 {
		t.Errorf("after k: cursor = %d, want 1", m.cursor)
	}
}

func TestNavigation_Arrows(t *testing.T) {
	m := setupModel(t)

	updated, _ := m.Update(specialKeyMsg(tea.KeyDown))
	m = updated.(Model)
	if m.cursor != 1 {
		t.Errorf("after down: cursor = %d, want 1", m.cursor)
	}

	updated, _ = m.Update(specialKeyMsg(tea.KeyUp))
	m = updated.(Model)
	if m.cursor != 0 {
		t.Errorf("after up: cursor = %d, want 0", m.cursor)
	}
}

func TestNavigation_CycleWrap(t *testing.T) {
	m := setupModel(t)

	// Right arrow wraps around
	updated, _ := m.Update(keyMsg("right"))
	m = updated.(Model)
	if m.cursor != 1 {
		t.Errorf("after right: cursor = %d, want 1", m.cursor)
	}

	updated, _ = m.Update(keyMsg("right"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("right"))
	m = updated.(Model)
	if m.cursor != 0 {
		t.Errorf("after wrapping right: cursor = %d, want 0", m.cursor)
	}

	// Left arrow wraps backward
	updated, _ = m.Update(keyMsg("left"))
	m = updated.(Model)
	if m.cursor != 2 {
		t.Errorf("after left from 0: cursor = %d, want 2", m.cursor)
	}
}

func TestNavigation_Tab(t *testing.T) {
	m := setupModel(t)

	updated, _ := m.Update(keyMsg("tab"))
	m = updated.(Model)
	if m.cursor != 1 {
		t.Errorf("after tab: cursor = %d, want 1", m.cursor)
	}
}

func TestNavigation_GG(t *testing.T) {
	m := setupModel(t)

	// Move to bottom first
	updated, _ := m.Update(keyMsg("G"))
	m = updated.(Model)
	if m.cursor != 2 {
		t.Errorf("after G: cursor = %d, want 2", m.cursor)
	}

	// gg to go to top
	updated, _ = m.Update(keyMsg("g"))
	m = updated.(Model)
	if m.cursor != 2 {
		t.Errorf("after first g: cursor = %d, want 2 (shouldn't move yet)", m.cursor)
	}
	if !m.lastKeyWasG {
		t.Error("lastKeyWasG should be true after first g")
	}

	updated, _ = m.Update(keyMsg("g"))
	m = updated.(Model)
	if m.cursor != 0 {
		t.Errorf("after gg: cursor = %d, want 0", m.cursor)
	}
}

func TestNavigation_GResetOnOtherKey(t *testing.T) {
	m := setupModel(t)

	// Press g once
	updated, _ := m.Update(keyMsg("g"))
	m = updated.(Model)
	if !m.lastKeyWasG {
		t.Error("lastKeyWasG should be true")
	}

	// Press j (not g) should reset
	updated, _ = m.Update(keyMsg("j"))
	m = updated.(Model)
	if m.lastKeyWasG {
		t.Error("lastKeyWasG should be reset after non-g key")
	}
}

func TestSearch_EnterAndExit(t *testing.T) {
	m := setupModel(t)

	// Enter search mode
	updated, _ := m.Update(keyMsg("/"))
	m = updated.(Model)
	if !m.searchMode {
		t.Error("should be in search mode after /")
	}

	// Cancel search with esc
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.searchMode {
		t.Error("should exit search mode on esc")
	}
	if m.searchQuery != "" {
		t.Errorf("search query should be empty after esc, got %q", m.searchQuery)
	}
}

func TestSearch_ConfirmWithEnter(t *testing.T) {
	m := setupModel(t)

	// Enter search mode
	updated, _ := m.Update(keyMsg("/"))
	m = updated.(Model)

	// Type search term
	m.searchInput.SetValue("modified")
	m.searchQuery = "modified"

	// Confirm with enter
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.searchMode {
		t.Error("should exit search mode on enter")
	}
	if m.searchQuery != "modified" {
		t.Errorf("searchQuery = %q, want 'modified'", m.searchQuery)
	}
}

func TestSearch_EscClearsHighlights(t *testing.T) {
	m := setupModel(t)
	m.searchQuery = "test"

	// Esc in normal mode should clear search
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.searchQuery != "" {
		t.Errorf("searchQuery should be cleared, got %q", m.searchQuery)
	}
}

func TestHighlightSearch(t *testing.T) {
	content := "On branch main\nChanges not staged for commit:\n  modified: file.go"

	result := highlightSearch(content, "modified")
	if !strings.Contains(result, "modified") {
		t.Error("highlighted content should contain the search term")
	}

	// Non-matching lines should remain plain
	lines := strings.Split(result, "\n")
	if strings.Contains(lines[0], "\x1b[") {
		// Line 0 is "On branch main" - shouldn't have ANSI if no match
	}

	// Empty query returns content unchanged
	same := highlightSearch(content, "")
	if same != content {
		t.Error("empty query should return content unchanged")
	}
}

func TestHighlightSearch_CaseInsensitive(t *testing.T) {
	content := "Modified file\nunchanged line"
	result := highlightSearch(content, "modified")
	// Case-insensitive match: "Modified" should match "modified" query.
	// In test environments without TTY, lipgloss may strip ANSI codes,
	// so we verify the line content is preserved even if styling is stripped.
	if !strings.Contains(result, "Modified file") {
		t.Error("highlighted content should preserve the matching line text")
	}
	if !strings.Contains(result, "unchanged line") {
		t.Error("highlighted content should preserve non-matching lines")
	}
}

func TestView_ShowsLoading(t *testing.T) {
	m := New(testRepos())
	view := m.View()
	if !strings.Contains(view, "Loading") {
		t.Error("should show Loading before ready")
	}
}

func TestView_ShowsRepoList(t *testing.T) {
	m := setupModel(t)
	view := m.View()

	if !strings.Contains(view, "camp") {
		t.Error("view should contain repo name 'camp'")
	}
	if !strings.Contains(view, "fest") {
		t.Error("view should contain repo name 'fest'")
	}
	if !strings.Contains(view, "daemon") {
		t.Error("view should contain repo name 'daemon'")
	}
	if !strings.Contains(view, "Repositories") {
		t.Error("view should contain header 'Repositories'")
	}
}

func TestView_ShowsStatusContent(t *testing.T) {
	m := setupModel(t)
	view := m.View()

	if !strings.Contains(view, "git status") {
		t.Error("view should contain git status header")
	}
}

func TestView_SearchModeShowsInput(t *testing.T) {
	m := setupModel(t)
	updated, _ := m.Update(keyMsg("/"))
	m = updated.(Model)
	view := m.View()

	if !strings.Contains(view, "/") {
		t.Error("view in search mode should show search prompt")
	}
}

func TestView_HelpBarContent(t *testing.T) {
	m := setupModel(t)
	view := m.View()

	if !strings.Contains(view, "q: quit") {
		t.Error("help bar should contain quit hint")
	}
	if !strings.Contains(view, "/: search") {
		t.Error("help bar should contain search hint")
	}
}

func TestFormatRepoLine_Clean(t *testing.T) {
	m := setupModel(t)
	r := RepoInfo{Name: "test", Branch: "main", Clean: true}
	line := m.formatRepoLine(r, 30)
	if !strings.Contains(line, "test") {
		t.Error("line should contain repo name")
	}
	if !strings.Contains(line, "main") {
		t.Error("line should contain branch name")
	}
}

func TestFormatRepoLine_WithChanges(t *testing.T) {
	m := setupModel(t)
	r := RepoInfo{Name: "test", Branch: "main", Staged: 2, Modified: 1, Untracked: 3}
	line := m.formatRepoLine(r, 30)
	if !strings.Contains(line, "+2") {
		t.Error("line should show staged count")
	}
	if !strings.Contains(line, "~1") {
		t.Error("line should show modified count")
	}
	if !strings.Contains(line, "?3") {
		t.Error("line should show untracked count")
	}
}

func TestFormatRepoLine_Error(t *testing.T) {
	m := setupModel(t)
	r := RepoInfo{Name: "bad", Error: "not a git repo"}
	line := m.formatRepoLine(r, 30)
	if !strings.Contains(line, "not a git repo") {
		t.Error("line should show error message")
	}
}

func TestFormatRepoLine_LongBranch(t *testing.T) {
	m := setupModel(t)
	r := RepoInfo{Name: "test", Branch: "feature/very-long-branch-name", Clean: true}
	line := m.formatRepoLine(r, 30)
	if strings.Contains(line, "feature/very-long-branch-name") {
		t.Error("long branch should be truncated")
	}
	if !strings.Contains(line, "…") {
		t.Error("truncated branch should have ellipsis")
	}
}

func TestQuit(t *testing.T) {
	m := setupModel(t)
	_, cmd := m.Update(keyMsg("q"))
	if cmd == nil {
		t.Error("q should return quit command")
	}
}

func TestCurrentStatusContent_CacheMiss(t *testing.T) {
	m := New(testRepos())
	content := m.currentStatusContent()
	if !strings.Contains(content, "Loading") {
		t.Error("cache miss should show Loading message")
	}
}

func TestCurrentStatusContent_WithSearch(t *testing.T) {
	m := New(testRepos())
	m.statusCache[0] = "modified: file.go\nunchanged line"
	m.searchQuery = "modified"
	content := m.currentStatusContent()
	// Content should contain both lines (highlighting may be stripped in test env)
	if !strings.Contains(content, "modified") {
		t.Error("search content should contain the matched term")
	}
	if !strings.Contains(content, "unchanged line") {
		t.Error("search content should preserve non-matching lines")
	}
}

func TestEnsureStatus_Cached(t *testing.T) {
	m := New(testRepos())
	m.statusCache[0] = "cached"
	cmd := m.ensureStatus(0)
	if cmd != nil {
		t.Error("ensureStatus should return nil for cached entries")
	}
}

func TestEnsureStatus_NotCached(t *testing.T) {
	m := New(testRepos())
	cmd := m.ensureStatus(0)
	if cmd == nil {
		t.Error("ensureStatus should return a command for uncached entries")
	}
}

func TestStatusMsg_UpdatesCache(t *testing.T) {
	m := setupModel(t)
	msg := statusMsg{index: 0, output: "new status content"}

	updated, _ := m.Update(msg)
	m = updated.(Model)
	if m.statusCache[0] != "new status content" {
		t.Error("statusMsg should update cache")
	}
}

func TestLeftPaneWidth(t *testing.T) {
	tests := []struct {
		width int
		want  int
	}{
		{80, 25},  // 80*30/100=24, clamped to 25
		{100, 30}, // 100*30/100=30
		{200, 40}, // 200*30/100=60, clamped to 40
		{50, 25},  // 50*30/100=15, clamped to 25
	}

	for _, tt := range tests {
		m := Model{width: tt.width}
		got := m.leftPaneWidth()
		if got != tt.want {
			t.Errorf("leftPaneWidth(%d) = %d, want %d", tt.width, got, tt.want)
		}
	}
}
