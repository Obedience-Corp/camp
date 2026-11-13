package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/ui/uitest"
)

const approvalsLongBody = "Step 2 of 2 needs a decision.\n\nGoal: Get sign-off.\n\n" +
	"Note 1: check the restore path.\n\nNote 2: check the restore path.\n\nNote 3: check the restore path.\n\n" +
	"Note 4: check the restore path.\n\nNote 5: check the restore path.\n\nNote 6: check the restore path.\n\n" +
	"Note 7: check the restore path.\n\nNote 8: check the restore path.\n\nLast line of the request."

func approvalRows(n int) []approvalRow {
	rows := make([]approvalRow, n)
	for i := range rows {
		rows[i] = approvalFixture(fmt.Sprintf("ap-%d", i+1), true)
		rows[i].Title = fmt.Sprintf("demo %d: REVIEW", i+1)
	}
	return rows
}

func approvalsSized(m approvalsTUIModel, w, h int) approvalsTUIModel {
	m.width, m.height = w, h
	return m
}

func approvalsLastLine(view string) string {
	lines := strings.Split(view, "\n")
	return lines[len(lines)-1]
}

func TestApprovalsTUI_EveryScreenFitsTheTerminal(t *testing.T) {
	long := approvalFixture("ap-long", true)
	long.Body = approvalsLongBody
	blocked := approvalFixture("ap-blocked", false)
	noticed := approvalsModel(approvalRows(3)...)
	noticed.snap.behindKnown, noticed.snap.behind, noticed.snap.uncommitted = true, 3, 1
	confirm, _ := press(t, approvalsModel(long), "a")
	reason, _ := press(t, approvalsModel(long), "x", "n", "o", "space", "t", "e", "s", "t", "s")
	screens := map[string]approvalsTUIModel{
		"empty":          approvalsModel(),
		"one":            approvalsModel(approvalRows(1)...),
		"many":           approvalsModel(approvalRows(30)...),
		"long body":      approvalsModel(long),
		"not actionable": approvalsModel(blocked),
		"notices":        noticed,
		"confirm":        confirm,
		"reason":         reason,
	}
	sizes := [][2]int{{160, 50}, {100, 30}, {80, 24}, {60, 14}, {50, 12}, {30, 8}}
	for name, m := range screens {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("%s %dx%d", name, size[0], size[1]), func(t *testing.T) {
				uitest.AssertBounded(t, approvalsSized(m, size[0], size[1]).View(), size[0], size[1])
			})
		}
	}
}

func TestApprovalsTUI_KeysStayOnTheLastRow(t *testing.T) {
	long := approvalFixture("ap-long", true)
	long.Body = approvalsLongBody
	for _, size := range [][2]int{{160, 50}, {100, 30}, {80, 24}, {50, 12}} {
		view := approvalsSized(approvalsModel(long), size[0], size[1]).View()
		uitest.AssertBounded(t, view, size[0], size[1])
		if last := approvalsLastLine(view); !strings.Contains(last, "approve") || !strings.Contains(last, "reject") || !strings.Contains(last, "quit") {
			t.Fatalf("%dx%d: a long request pushed the keys off the last row: %q", size[0], size[1], last)
		}
	}
}

func TestApprovalsTUI_WideTerminalPutsTheRequestBesideTheList(t *testing.T) {
	wide := approvalsSized(approvalsModel(approvalRows(3)...), 140, 30).View()
	narrow := approvalsSized(approvalsModel(approvalRows(3)...), 80, 30).View()
	beside := func(view string) bool {
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, "Waiting (3)") && strings.Contains(line, "Request ap-1") {
				return true
			}
		}
		return false
	}
	if !beside(wide) {
		t.Fatalf("at 140 columns the request should sit beside the list:\n%s", wide)
	}
	if beside(narrow) || !strings.Contains(narrow, "Waiting (3)") || !strings.Contains(narrow, "Request ap-1") {
		t.Fatalf("at 80 columns the request should sit below the list:\n%s", narrow)
	}
}

func TestApprovalsTUI_TitleIsShownOnceInTheRequest(t *testing.T) {
	row := approvalFixture("ap-1", true)
	lines := approvalRequestLines(row, time.Now(), 80)
	joined := strings.Join(lines, "\n")
	if strings.Count(joined, row.Title) != 1 {
		t.Fatalf("the request should name its title once:\n%s", joined)
	}
	if strings.Contains(joined, "To approve") || strings.Contains(joined, "from a terminal") {
		t.Fatalf("the browser approves with a key; it must not tell the user to run a command:\n%s", joined)
	}
	for _, want := range []string{"workflow", "step 2 REVIEW", "bot (agent)", "wabc", "workflow/explore/demo", "Goal: Get sign-off."} {
		if !strings.Contains(joined, want) {
			t.Fatalf("request is missing %q:\n%s", want, joined)
		}
	}
}

func TestApprovalsTUI_NotActionableReplacesTheDecisionKeys(t *testing.T) {
	view := approvalsSized(approvalsModel(approvalFixture("ap-1", false)), 120, 30).View()
	if !strings.Contains(view, "Not actionable here: its directory is not on this machine") {
		t.Fatalf("the reason must be shown with the request:\n%s", view)
	}
	last := approvalsLastLine(view)
	if strings.Contains(last, "approve") || strings.Contains(last, "reject") {
		t.Fatalf("keys that cannot work must not be offered: %q", last)
	}
	if !strings.Contains(last, "not actionable here") || !strings.Contains(last, "open document") {
		t.Fatalf("key bar = %q", last)
	}
}

func TestApprovalsTUI_LongRequestScrollsInsideItsPane(t *testing.T) {
	long := approvalFixture("ap-long", true)
	long.Body = approvalsLongBody
	m := approvalsSized(approvalsModel(long, approvalFixture("ap-2", true)), 120, 20)
	top := m.View()
	if strings.Contains(top, "Last line of the request.") || !strings.Contains(top, "lines") || !strings.Contains(approvalsLastLine(top), "scroll") {
		t.Fatalf("a request longer than its pane should be cut with a line count and a scroll key:\n%s", top)
	}
	for i := 0; i < 10; i++ {
		m, _ = press(t, m, "pgdown")
	}
	bottom := m.View()
	if !strings.Contains(bottom, "Last line of the request.") {
		t.Fatalf("paging down should reach the end:\n%s", bottom)
	}
	visible, total := m.requestViewport()
	if m.scroll != total-visible {
		t.Fatalf("scroll = %d, want it clamped to %d", m.scroll, total-visible)
	}
	if m, _ = press(t, m, "j"); m.scroll != 0 {
		t.Fatalf("moving to another request must start it at the top, scroll = %d", m.scroll)
	}
}

func TestApprovalsTUI_StatusDescribesOnlyTheLastKey(t *testing.T) {
	m, _ := press(t, approvalsModel(approvalFixture("ap-1", true)), "a", "n")
	if m.status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", m.status)
	}
	if m, _ = press(t, m, "pgdown"); m.status != "" {
		t.Fatalf("a stale status must clear on the next key, got %q", m.status)
	}
}

func TestApprovalsTUI_ConfirmShowsTheWholeCommandAndDirectory(t *testing.T) {
	row := approvalFixture("ap-1", true)
	row.ready.Dir = "/camp/workflow/explore/" + strings.Repeat("a-very-long-directory-name/", 6) + "demo"
	m, _ := press(t, approvalsModel(row), "x", "b", "a", "d", "enter")
	if m.mode != approvalsConfirm {
		t.Fatalf("mode = %v, want the confirm", m.mode)
	}
	for _, size := range [][2]int{{60, 40}, {60, 14}, {50, 12}, {40, 12}} {
		view := approvalsSized(m, size[0], size[1]).View()
		uitest.AssertBounded(t, view, size[0], size[1])
		var text strings.Builder
		for _, line := range strings.Split(view, "\n") {
			text.WriteString(strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "│")))
		}
		flat := strings.ReplaceAll(text.String(), " ", "")
		for _, want := range append([]string{row.ready.Dir, "run it", "cancel"}, m.pendingArgv...) {
			if !strings.Contains(flat, strings.ReplaceAll(want, " ", "")) {
				t.Fatalf("%dx%d: the confirm cut %q off:\n%s", size[0], size[1], want, view)
			}
		}
	}
}

func TestApprovalsTUI_PanesAreNoTallerThanTheLongestRequest(t *testing.T) {
	view := approvalsSized(approvalsModel(approvalFixture("ap-1", true)), 120, 40).View()
	rows := len(strings.Split(view, "\n"))
	if rows > 14 {
		t.Fatalf("one short request should not fill a 40-row terminal with empty boxes, got %d rows:\n%s", rows, view)
	}
	if last := approvalsLastLine(view); !strings.Contains(last, "approve") {
		t.Fatalf("the keys should follow the panes: %q", last)
	}
}

func TestApprovalRequestLines_LabelsAndTabs(t *testing.T) {
	row := approvalFixture("ap-1", true)
	row.Body = "Goal: ship it\nSee https://example.com/x for more\n\tindented with a tab"
	joined := strings.Join(approvalRequestLines(row, time.Now(), 80), "\n")
	if strings.Contains(joined, "\t") {
		t.Fatalf("tabs must be expanded before the text is measured:\n%q", joined)
	}
	if approvalLabelRe.MatchString("See https://example.com/x") || !approvalLabelRe.MatchString("Goal: ship it") || !approvalLabelRe.MatchString("Actions:") {
		t.Fatal("only a word followed by a colon and a space or line end is a label")
	}
}

func TestApprovalsTUI_CompactViewNamesTheRequest(t *testing.T) {
	view := approvalsSized(approvalsModel(approvalFixture("ap-1", true)), 50, 12).View()
	if !strings.Contains(view, "Request ap-1") {
		t.Fatalf("a small terminal must still show the request ID:\n%s", view)
	}
}

func TestApprovalsTUI_UnknownSizeShowsEverything(t *testing.T) {
	long := approvalFixture("ap-long", true)
	long.Body = approvalsLongBody
	view := approvalsModel(append(approvalRows(12), long)...).View()
	for _, want := range []string{"demo 1: REVIEW", "demo 12: REVIEW", "Waiting (13)", "quit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("with no size reported the view should hold %q:\n%s", want, view)
		}
	}
}

func TestApprovalWrapSplitsWhatCannotWrap(t *testing.T) {
	got := approvalWrap("in /a/path/with/no/spaces/at/all", 10)
	for _, line := range got {
		if len([]rune(line)) > 10 {
			t.Fatalf("line %q is wider than 10: %q", line, got)
		}
	}
	if strings.Join(got, "") != "in/a/path/with/no/spaces/at/all" {
		t.Fatalf("wrapping lost text: %q", got)
	}
}
