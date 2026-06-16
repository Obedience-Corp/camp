package explorer

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
)

func makeReadyModel() Model {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)
	m.ready = true
	m.width = 120
	m.height = 30

	readyIntent := &intent.Intent{
		ID:        "ready-1",
		Title:     "Implement search",
		Status:    intent.StatusReady,
		Type:      intent.TypeFeature,
		CreatedAt: time.Now(),
	}
	m.intents = []*intent.Intent{readyIntent}
	m.filteredIntents = m.intents
	m.groups = groupIntentsByStatus(m.intents, false)
	m.cursorGroup = 1
	m.cursorItem = 0
	return m
}

func asExplorerModel(t *testing.T, tm tea.Model) Model {
	t.Helper()
	switch v := tm.(type) {
	case Model:
		return v
	case *Model:
		return *v
	default:
		t.Fatalf("unexpected model type %T", tm)
		return Model{}
	}
}

func TestPromoteAction_PressP_ReadyIntent_ShowsPicker(t *testing.T) {
	m := makeReadyModel()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = asExplorerModel(t, updated)

	if m.focus != focusPromoteTarget {
		t.Fatalf("focus = %v, want focusPromoteTarget after pressing p on ready intent", m.focus)
	}
	if m.promoteTargetIntent == nil {
		t.Fatal("promoteTargetIntent should be set when showing picker")
	}
}

func TestPromoteAction_ConfirmWithLegacyPromoteAction_RoutesToPicker(t *testing.T) {
	m := makeReadyModel()

	readyIntent := m.intents[0]

	m.focus = focusConfirm
	m.pendingAction = "promote"
	m.pendingIntent = readyIntent
	m.confirmDialog = tui.NewConfirmationDialog("Promote to Festival", "Promote?")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asExplorerModel(t, updated)

	if m.focus != focusPromoteTarget {
		t.Fatalf("focus = %v, want focusPromoteTarget; legacy 'promote' confirm should route to picker, not skip it", m.focus)
	}
	if m.promoteTargetIntent == nil {
		t.Fatal("promoteTargetIntent should be set when routing legacy promote confirm to picker")
	}
}
