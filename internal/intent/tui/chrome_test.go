package tui

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/obey-shared/brand"
)

func TestHeader_ContainsBrandMarkAndTitle(t *testing.T) {
	out := Header("explore", "3 intents", 80)
	if !strings.Contains(out, brand.CompactMark) {
		t.Fatalf("header missing compact mark %q:\n%s", brand.CompactMark, out)
	}
	if !strings.Contains(out, "intent") {
		t.Fatalf("header missing brand word:\n%s", out)
	}
	if !strings.Contains(out, "explore") {
		t.Fatalf("header missing title:\n%s", out)
	}
	if !strings.Contains(out, "3 intents") {
		t.Fatalf("header missing right label:\n%s", out)
	}
	// rule is second line
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("want header + rule, got %d lines", len(lines))
	}
	if !strings.Contains(lines[1], "─") {
		t.Fatalf("second line should be rule, got %q", lines[1])
	}
}

func TestFooter_RuleAndHints(t *testing.T) {
	out := Footer("j/k nav · q quit", 60)
	if !strings.Contains(out, "─") {
		t.Fatalf("footer missing rule:\n%s", out)
	}
	if !strings.Contains(out, "j/k nav") {
		t.Fatalf("footer missing hints:\n%s", out)
	}
}

func TestStepPills_CurrentHighlighted(t *testing.T) {
	out := StepPills([]string{"Title", "Type", "Body"}, 1)
	// Should contain all step labels
	for _, s := range []string{"Title", "Type", "Body"} {
		if !strings.Contains(out, s) {
			t.Fatalf("pills missing %q:\n%s", s, out)
		}
	}
}

func TestFocusCursor_UsesBrandFireStyle(t *testing.T) {
	// FocusCursor must route through brandStyles.Fire (not the bare
	// CursorIndicator constant). In plain/NO_COLOR terminals Fire.Render may
	// equal the glyph, so compare to the styled helper itself.
	want := brandStyles.Fire.Render("▸")
	if got := FocusCursor(); got != want {
		t.Fatalf("FocusCursor() = %q, want brand Fire-rendered caret %q", got, want)
	}
	if EmptyCursor() != " " {
		t.Fatalf("EmptyCursor() = %q, want single space", EmptyCursor())
	}
}
