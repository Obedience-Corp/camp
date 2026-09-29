package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestChecklistRowStatusColumnIsShared(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteChecklistRow(&buf, 80, "  ", "── ", "Checkout main", "already on it", StatusMuted); err != nil {
		t.Fatal(err)
	}
	if err := WriteChecklistRow(&buf, 80, "  ", "── ", "Fetch origin", "done", StatusSuccess); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2\n%s", len(lines), buf.String())
	}
	left := stripANSI(lines[0])
	right := stripANSI(lines[1])
	if got, want := strings.Index(left, "already on it"), strings.Index(right, "done"); got != want || got < 0 {
		t.Fatalf("status columns differ: %q vs %q", left, right)
	}
}

func TestChecklistRowWrapsOnWords(t *testing.T) {
	var buf bytes.Buffer
	status := "deleted 4 branches, removed 4 worktrees"
	if err := WriteChecklistRow(&buf, 60, "  ", "── ", "Prune merged branches", status, StatusSuccess); err != nil {
		t.Fatal(err)
	}
	plain := stripANSI(buf.String())
	if strings.Contains(plain, "branche\n") || strings.Contains(plain, "worktre\n") {
		t.Fatalf("wrapped inside a word:\n%s", plain)
	}
	if !strings.Contains(plain, "deleted 4 branches,") || !strings.Contains(plain, "removed 4 worktrees") {
		t.Fatalf("lost status text:\n%s", plain)
	}
	for _, line := range strings.Split(plain, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if lipgloss.Width(line) > 60 {
			t.Fatalf("line wider than 60: %q", line)
		}
	}
}

func TestChecklistRowKeepsALongTokenIntact(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteChecklistRow(&buf, 50, "  ", "── ", "Prune merged branches", "deleted fix-leverage-worktrees", StatusSuccess); err != nil {
		t.Fatal(err)
	}
	plain := stripANSI(buf.String())
	found := false
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "fix-leverage-worktrees") {
			found = true
		}
		if strings.Contains(line, "fix-l") && !strings.Contains(line, "fix-leverage-worktrees") {
			t.Fatalf("split the branch name: %q", line)
		}
	}
	if !found {
		t.Fatalf("branch name missing:\n%s", plain)
	}
}

func TestChecklistDetailWrapsUnderTheIndent(t *testing.T) {
	var buf bytes.Buffer
	text := "workflow/design/camp-immerse-color-profiles (design): not moved, design awaits implementation evidence (a merged branch or a completed festival)"
	if err := WriteChecklistDetail(&buf, 72, "     ", text); err != nil {
		t.Fatal(err)
	}
	plain := buf.String()
	if !strings.Contains(plain, "not moved") || !strings.Contains(plain, "completed festival") {
		t.Fatalf("detail dropped the reason:\n%s", plain)
	}
	for _, line := range strings.Split(strings.TrimRight(plain, "\n"), "\n") {
		if !strings.HasPrefix(line, "     ") {
			t.Fatalf("detail is not indented: %q", line)
		}
		if lipgloss.Width(line) > 72 {
			t.Fatalf("detail wider than 72: %q", line)
		}
	}
}

func TestChecklistMarkWidthsMatch(t *testing.T) {
	for _, tone := range []StatusTone{StatusPlain, StatusMuted, StatusSuccess, StatusWarning, StatusError, StatusInfo} {
		if got := lipgloss.Width(ChecklistMark(tone)); got != 2 {
			t.Fatalf("tone %d mark width = %d, want 2 (%q)", tone, got, stripANSI(ChecklistMark(tone)))
		}
	}
}

func TestChecklistSectionRuleEndsAtTheStatusColumn(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteChecklistSection(&buf, 88, "Sync", ""); err != nil {
		t.Fatal(err)
	}
	plain := stripANSI(buf.String())
	line := strings.Trim(plain, "\n")
	statusCol := lipgloss.Width(ChecklistRowIndent) + 2 + ChecklistLabelWidth
	if got := lipgloss.Width(line); got != statusCol {
		t.Fatalf("section width = %d, want %d\n%s", got, statusCol, line)
	}
	if !strings.Contains(line, "Sync") {
		t.Fatalf("section dropped the title: %q", line)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		for i < len(s) && s[i] != 'm' {
			i++
		}
	}
	return b.String()
}
