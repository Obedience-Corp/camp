package notify

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/notice"
)

const (
	legacyID = notice.DungeonLegacyID
	linksID  = notice.StaleLinksID
)

var (
	rootAID = notice.SubjectID(notice.KindNeverSynced, "data/a")
	rootBID = notice.SubjectID(notice.KindNeverSynced, "data/b")
)

var dismissedAt = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// fakeStore keeps dismissals in memory and reports notices the way the real
// detectors do, including the artifact detectors' habit of reporting only the
// first root nobody has dismissed.
type fakeStore struct {
	notices    []notice.Notice
	firstOnly  map[string]bool
	subjects   map[string]string
	dismissals *notice.DismissalFile
	writes     []string
	writeErr   error
	reloadErr  error
}

func newFakeStore(notices ...notice.Notice) *fakeStore {
	return &fakeStore{
		notices:    notices,
		firstOnly:  map[string]bool{},
		subjects:   map[string]string{rootAID: "data/a", rootBID: "data/b"},
		dismissals: &notice.DismissalFile{Version: 1, Dismissed: map[string]time.Time{}},
	}
}

func (f *fakeStore) Inventory(context.Context) (notice.Inventory, error) {
	if f.reloadErr != nil {
		return notice.Inventory{}, f.reloadErr
	}
	var detected []notice.Notice
	reported := false
	for _, n := range f.notices {
		if f.firstOnly[n.ID] {
			if reported || f.dismissals.IsDismissed(n.ID) {
				continue
			}
			reported = true
		}
		detected = append(detected, n)
	}
	return notice.Partition(detected, f.dismissals, f.subjects), nil
}

func (f *fakeStore) Dismiss(_ context.Context, id string) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.writes = append(f.writes, "dismiss "+id)
	f.dismissals.Dismiss(id, dismissedAt)
	return nil
}

func (f *fakeStore) Restore(_ context.Context, id string) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.writes = append(f.writes, "restore "+id)
	f.dismissals.Restore(id)
	return nil
}

type fakeClipboard struct {
	copied []string
	err    error
}

func (c *fakeClipboard) write(s string) error {
	if c.err != nil {
		return c.err
	}
	c.copied = append(c.copied, s)
	return nil
}

func legacyNotice() notice.Notice {
	return notice.Notice{ID: legacyID, Message: "this camp uses the visible dungeon/ layout", Command: "camp dungeon migrate"}
}

func linksNotice() notice.Notice {
	return notice.Notice{ID: linksID, Message: "1 workitem link points at a path that no longer exists", Command: "camp workitem doctor --fix"}
}

func rootNotice(id string) notice.Notice {
	subject := map[string]string{rootAID: "data/a", rootBID: "data/b"}[id]
	return notice.Notice{
		ID:      id,
		Subject: subject,
		Message: subject + " has never synced",
		Command: notice.ElsewherePrefix + "camp sync --from <id of fixture> --artifacts-only   (dismiss: camp notify dismiss " + id + ")",
	}
}

func setup(t *testing.T, store *fakeStore, clip *fakeClipboard) Model {
	t.Helper()
	inv, err := store.Inventory(context.Background())
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if clip == nil {
		clip = &fakeClipboard{}
	}
	m := New(context.Background(), inv, Options{Store: store, Clipboard: clip.write})
	return update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
}

func update(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(Model)
}

func press(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+c":
		msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// act presses key and, when that starts a write, runs it and feeds the
// result back the way the Bubble Tea runtime would.
func act(t *testing.T, m Model, key string) Model {
	t.Helper()
	m, cmd := press(t, m, key)
	if cmd == nil {
		return m
	}
	if !m.busy {
		t.Fatalf("%q returned a command without marking the model busy", key)
	}
	return update(t, m, cmd())
}

func liveIDs(m Model) []string {
	ids := make([]string, 0, len(m.inv.Live))
	for _, n := range m.inv.Live {
		ids = append(ids, n.ID)
	}
	return ids
}

func dismissedIDs(m Model) []string {
	ids := make([]string, 0, len(m.inv.Dismissed))
	for _, d := range m.inv.Dismissed {
		ids = append(ids, d.ID)
	}
	return ids
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func teaSize(w, h int) tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: w, Height: h}
}
