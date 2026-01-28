package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/obediencecorp/camp/internal/intent"
)

// mockIntents creates test intents for viewer navigation tests.
func mockIntents(count int) []*intent.Intent {
	intents := make([]*intent.Intent, count)
	for i := range intents {
		intents[i] = &intent.Intent{
			ID:     string(rune('a' + i)),
			Title:  "Intent " + string(rune('A'+i)),
			Status: intent.StatusInbox,
		}
	}
	return intents
}

func TestViewer_SiblingNavigation(t *testing.T) {
	ctx := context.Background()
	siblings := mockIntents(5)
	currentIndex := 2 // Start at intent C (index 2)

	m := NewIntentViewerModel(ctx, siblings[currentIndex], siblings, currentIndex, nil, 80, 24)

	// Verify initial state
	if m.currentIndex != 2 {
		t.Errorf("Expected initial index 2, got %d", m.currentIndex)
	}
	if m.intent.ID != "c" {
		t.Errorf("Expected initial intent ID 'c', got %q", m.intent.ID)
	}

	// Navigate right
	m.navigateNext()
	if m.currentIndex != 3 {
		t.Errorf("Expected index 3 after right, got %d", m.currentIndex)
	}
	if m.intent.ID != "d" {
		t.Errorf("Expected intent ID 'd', got %q", m.intent.ID)
	}

	// Navigate left
	m.navigatePrev()
	if m.currentIndex != 2 {
		t.Errorf("Expected index 2 after left, got %d", m.currentIndex)
	}
	if m.intent.ID != "c" {
		t.Errorf("Expected intent ID 'c', got %q", m.intent.ID)
	}
}

func TestViewer_NavigationWrapAround(t *testing.T) {
	ctx := context.Background()
	siblings := mockIntents(3)

	// Start at last intent
	m := NewIntentViewerModel(ctx, siblings[2], siblings, 2, nil, 80, 24)

	// Navigate right should wrap to first
	m.navigateNext()
	if m.currentIndex != 0 {
		t.Errorf("Expected wrap to index 0, got %d", m.currentIndex)
	}
	if m.intent.ID != "a" {
		t.Errorf("Expected intent ID 'a', got %q", m.intent.ID)
	}

	// Navigate left should wrap to last
	m.navigatePrev()
	if m.currentIndex != 2 {
		t.Errorf("Expected wrap to index 2, got %d", m.currentIndex)
	}
	if m.intent.ID != "c" {
		t.Errorf("Expected intent ID 'c', got %q", m.intent.ID)
	}
}

func TestViewer_KeyNavigation(t *testing.T) {
	ctx := context.Background()
	siblings := mockIntents(3)

	m := NewIntentViewerModel(ctx, siblings[0], siblings, 0, nil, 80, 24)

	// Test right arrow key
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(IntentViewerModel)
	if m.currentIndex != 1 {
		t.Errorf("Expected index 1 after right key, got %d", m.currentIndex)
	}

	// Test 'l' key (vim-style right)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(IntentViewerModel)
	if m.currentIndex != 2 {
		t.Errorf("Expected index 2 after 'l' key, got %d", m.currentIndex)
	}

	// Test left arrow key
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(IntentViewerModel)
	if m.currentIndex != 1 {
		t.Errorf("Expected index 1 after left key, got %d", m.currentIndex)
	}

	// Test 'h' key (vim-style left)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(IntentViewerModel)
	if m.currentIndex != 0 {
		t.Errorf("Expected index 0 after 'h' key, got %d", m.currentIndex)
	}
}

func TestViewer_NavigationKeysReturnNilCmd(t *testing.T) {
	// Verify that navigation keys return nil cmd (not passed to viewport)
	// This prevents the freeze bug where keys fell through to viewport
	ctx := context.Background()
	siblings := mockIntents(3)

	m := NewIntentViewerModel(ctx, siblings[1], siblings, 1, nil, 80, 24)

	testCases := []struct {
		name string
		key  tea.KeyMsg
	}{
		{"left arrow", tea.KeyMsg{Type: tea.KeyLeft}},
		{"right arrow", tea.KeyMsg{Type: tea.KeyRight}},
		{"h key", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")}},
		{"l key", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, cmd := m.Update(tc.key)
			if cmd != nil {
				t.Errorf("Expected nil cmd after %s, got non-nil (key may have fallen through to viewport)", tc.name)
			}
		})
	}
}

func TestViewer_SingleIntentNoNavigation(t *testing.T) {
	ctx := context.Background()
	siblings := mockIntents(1) // Only one intent

	m := NewIntentViewerModel(ctx, siblings[0], siblings, 0, nil, 80, 24)

	// Navigation keys should not change anything with only one sibling
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(IntentViewerModel)
	if m.currentIndex != 0 {
		t.Errorf("Expected index to stay at 0 with single intent, got %d", m.currentIndex)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(IntentViewerModel)
	if m.currentIndex != 0 {
		t.Errorf("Expected index to stay at 0 with single intent, got %d", m.currentIndex)
	}
}

func TestViewer_ClosedMsgIncludesFinalIndex(t *testing.T) {
	ctx := context.Background()
	siblings := mockIntents(5)

	m := NewIntentViewerModel(ctx, siblings[1], siblings, 1, nil, 80, 24)

	// Navigate to a different position
	m.navigateNext()
	m.navigateNext()

	if m.currentIndex != 3 {
		t.Errorf("Expected current index 3, got %d", m.currentIndex)
	}

	// Simulate closing and check the message
	closeCmd := m.closeViewer()
	msg := closeCmd().(viewerClosedMsg)

	if msg.finalIndex != 3 {
		t.Errorf("Expected finalIndex 3 in closed message, got %d", msg.finalIndex)
	}
}

func TestViewer_FooterShowsPositionWithMultipleSiblings(t *testing.T) {
	ctx := context.Background()
	siblings := mockIntents(5)

	m := NewIntentViewerModel(ctx, siblings[2], siblings, 2, nil, 120, 24)

	footer := m.renderFooter()

	// Should contain position indicator "3/5"
	if !containsSubstring(footer, "3/5") {
		t.Errorf("Expected footer to contain '3/5', got: %s", footer)
	}

	// Should contain navigation hint
	if !containsSubstring(footer, "←/→") || !containsSubstring(footer, "prev/next") {
		t.Errorf("Expected footer to contain navigation hint, got: %s", footer)
	}
}

func TestViewer_FooterNoPositionWithSingleIntent(t *testing.T) {
	ctx := context.Background()
	siblings := mockIntents(1)

	m := NewIntentViewerModel(ctx, siblings[0], siblings, 0, nil, 120, 24)

	footer := m.renderFooter()

	// Should NOT contain position indicator
	if containsSubstring(footer, "1/1") {
		t.Errorf("Expected footer to NOT contain '1/1' with single intent, got: %s", footer)
	}

	// Should contain simple back hint
	if !containsSubstring(footer, "back to list") {
		t.Errorf("Expected footer to contain 'back to list', got: %s", footer)
	}
}

// containsSubstring is a helper to check if a string contains a substring.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstringHelper(s, substr))
}

func containsSubstringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
