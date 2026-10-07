package notify

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/notice"
)

func TestEmptyStateSaysNoNotices(t *testing.T) {
	m := setup(t, newFakeStore(), nil)

	view := m.View()
	if !strings.Contains(view, "No notices.") {
		t.Fatalf("empty view does not say so plainly:\n%s", view)
	}
	if strings.Contains(view, "d dismiss") {
		t.Errorf("empty view offers actions with nothing to act on:\n%s", view)
	}
	m, cmd := press(t, m, "d")
	if cmd != nil || m.status != "no notices to dismiss" || !m.statusErr {
		t.Errorf("d on an empty list: cmd=%v status=%q err=%v", cmd != nil, m.status, m.statusErr)
	}
	m, _ = press(t, m, "y")
	if m.status != "nothing to copy" {
		t.Errorf("y on an empty list: status=%q", m.status)
	}
}

func TestNavigationCrossesSectionsAndClamps(t *testing.T) {
	store := newFakeStore(legacyNotice(), linksNotice())
	store.dismissals.Dismiss(linksID, dismissedAt)
	m := setup(t, store, nil)

	if got := liveIDs(m); len(got) != 1 || got[0] != legacyID {
		t.Fatalf("live = %v, want [%s]", got, legacyID)
	}
	m, _ = press(t, m, "k")
	if m.cursor != 0 {
		t.Errorf("k at the top moved the cursor to %d", m.cursor)
	}
	m, _ = press(t, m, "j")
	if r, _ := m.selected(); r.id != linksID || r.section != sectionDismissed {
		t.Errorf("j from the last live row selected %+v, want the dismissed %s", r, linksID)
	}
	m, _ = press(t, m, "down")
	if m.cursor != 1 {
		t.Errorf("down past the end moved the cursor to %d", m.cursor)
	}
	m, _ = press(t, m, "g")
	if m.cursor != 0 {
		t.Errorf("g did not return to the first row, cursor=%d", m.cursor)
	}
	m, _ = press(t, m, "G")
	if m.cursor != 1 {
		t.Errorf("G did not reach the last row, cursor=%d", m.cursor)
	}
}

func TestDismissWritesAndMovesNoticeToDismissed(t *testing.T) {
	store := newFakeStore(legacyNotice(), linksNotice())
	m := setup(t, store, nil)

	m = act(t, m, "d")

	if len(store.writes) != 1 || store.writes[0] != "dismiss "+legacyID {
		t.Fatalf("writes = %v, want one dismissal of %s", store.writes, legacyID)
	}
	if got := liveIDs(m); len(got) != 1 || got[0] != linksID {
		t.Errorf("live after dismiss = %v, want [%s]", got, linksID)
	}
	if got := dismissedIDs(m); len(got) != 1 || got[0] != legacyID {
		t.Errorf("dismissed after dismiss = %v, want [%s]", got, legacyID)
	}
	if r, _ := m.selected(); r.id != linksID {
		t.Errorf("cursor left the live list: selected %s", r.id)
	}
	if m.busy || m.statusErr || !strings.Contains(m.status, "dismissed "+legacyID) {
		t.Errorf("status after dismiss: busy=%v err=%v %q", m.busy, m.statusErr, m.status)
	}
	if got := m.Changes(); len(got) != 1 || got[0] != (Change{ID: legacyID, Dismissed: true}) {
		t.Errorf("Changes() = %+v", got)
	}
}

func TestDismissRereadsDetectorsAndSurfacesTheNextRoot(t *testing.T) {
	store := newFakeStore(rootNotice(rootAID), rootNotice(rootBID))
	store.firstOnly[rootAID], store.firstOnly[rootBID] = true, true
	m := setup(t, store, nil)

	if got := liveIDs(m); len(got) != 1 || got[0] != rootAID {
		t.Fatalf("live = %v, want only the first root", got)
	}
	m = act(t, m, "d")

	if got := liveIDs(m); len(got) != 1 || got[0] != rootBID {
		t.Errorf("live after dismissing %s = %v, want the next root %s", rootAID, got, rootBID)
	}
}

func TestDismissOnADismissedRowWritesNothing(t *testing.T) {
	store := newFakeStore(linksNotice())
	store.dismissals.Dismiss(linksID, dismissedAt)
	m := setup(t, store, nil)

	m, cmd := press(t, m, "d")

	if cmd != nil || len(store.writes) != 0 {
		t.Fatalf("d on a dismissed row wrote: cmd=%v writes=%v", cmd != nil, store.writes)
	}
	if !m.statusErr || !strings.Contains(m.status, "already dismissed") {
		t.Errorf("status = %q (err=%v)", m.status, m.statusErr)
	}
}

func TestRestoreMovesNoticeBackToLive(t *testing.T) {
	store := newFakeStore(legacyNotice())
	store.dismissals.Dismiss(legacyID, dismissedAt)
	m := setup(t, store, nil)

	m = act(t, m, "r")

	if len(store.writes) != 1 || store.writes[0] != "restore "+legacyID {
		t.Fatalf("writes = %v, want one restore of %s", store.writes, legacyID)
	}
	if got := liveIDs(m); len(got) != 1 || got[0] != legacyID {
		t.Errorf("live after restore = %v", got)
	}
	if len(m.inv.Dismissed) != 0 {
		t.Errorf("dismissed after restore = %v", dismissedIDs(m))
	}
	if m.status != "restored "+legacyID {
		t.Errorf("status = %q", m.status)
	}
}

func TestRestoreOfAnUndetectedNoticeSaysSo(t *testing.T) {
	store := newFakeStore()
	store.dismissals.Dismiss(rootAID, dismissedAt)
	m := setup(t, store, nil)

	m = act(t, m, "r")

	if !m.inv.Empty() {
		t.Fatalf("inventory after restore = %+v", m.inv)
	}
	if !strings.Contains(m.status, "not detected right now") {
		t.Errorf("status = %q", m.status)
	}
	if !strings.Contains(m.View(), "No notices.") {
		t.Error("restoring the last dismissal did not fall back to the empty state")
	}
}

func TestRestoreOnALiveRowWritesNothing(t *testing.T) {
	store := newFakeStore(legacyNotice())
	m := setup(t, store, nil)

	m, cmd := press(t, m, "r")

	if cmd != nil || len(store.writes) != 0 {
		t.Fatalf("r on a live row wrote: %v", store.writes)
	}
	if !m.statusErr || !strings.Contains(m.status, "is not dismissed") {
		t.Errorf("status = %q", m.status)
	}
}

func TestDismissThenRestoreLeavesNoNetChange(t *testing.T) {
	store := newFakeStore(legacyNotice())
	m := setup(t, store, nil)

	m = act(t, m, "d")
	m = act(t, m, "r")

	if got := m.Changes(); len(got) != 0 {
		t.Errorf("Changes() after dismiss+restore = %+v, want none", got)
	}
}

func TestWriteFailureKeepsStateAndReports(t *testing.T) {
	store := newFakeStore(legacyNotice())
	store.writeErr = errors.New("disk full")
	m := setup(t, store, nil)

	m = act(t, m, "d")

	if got := liveIDs(m); len(got) != 1 {
		t.Errorf("live after failed dismiss = %v", got)
	}
	if !m.statusErr || m.status != "dismiss failed: disk full" {
		t.Errorf("status = %q (err=%v)", m.status, m.statusErr)
	}
	if len(m.Changes()) != 0 {
		t.Errorf("a failed write was recorded as a change: %+v", m.Changes())
	}
}

func TestReloadFailureStillRecordsTheWrite(t *testing.T) {
	store := newFakeStore(legacyNotice())
	m := setup(t, store, nil)
	store.reloadErr = errors.New("links.yaml unreadable")

	m = act(t, m, "d")

	if !m.statusErr || !strings.Contains(m.status, "dismissed "+legacyID+", but reloading notices failed") {
		t.Errorf("status = %q", m.status)
	}
	if len(m.Changes()) != 1 {
		t.Errorf("the dismissal landed but was not recorded: %+v", m.Changes())
	}
}

func TestCopyHandsOverTheBareFixCommand(t *testing.T) {
	clip := &fakeClipboard{}
	m := setup(t, newFakeStore(rootNotice(rootAID), linksNotice()), clip)

	m, _ = press(t, m, "y")
	m, _ = press(t, m, "j")
	m, _ = press(t, m, "c")

	want := []string{"camp sync --from <id of fixture> --artifacts-only", "camp workitem doctor --fix"}
	if len(clip.copied) != 2 || clip.copied[0] != want[0] || clip.copied[1] != want[1] {
		t.Fatalf("copied = %q, want %q", clip.copied, want)
	}
	if m.status != "copied camp workitem doctor --fix" {
		t.Errorf("status = %q", m.status)
	}
}

func TestCopyOfARemotePlaceholderFixSaysWhereAndWhat(t *testing.T) {
	m := setup(t, newFakeStore(rootNotice(rootAID)), nil)
	m, _ = press(t, m, "y")
	if m.status != "copied camp sync --from <id of fixture> --artifacts-only · fill in the <…> value and run it on another machine" {
		t.Errorf("status = %q", m.status)
	}
	if view := m.View(); !strings.Contains(view, "subject   data/a") {
		t.Errorf("detail pane does not name the subject:\n%s", view)
	}
}

func TestCopyRefusesADismissalWithNoFixOnRecord(t *testing.T) {
	clip := &fakeClipboard{}
	store := newFakeStore()
	store.dismissals.Dismiss(rootAID, dismissedAt)
	m := setup(t, store, clip)

	m, _ = press(t, m, "y")

	if len(clip.copied) != 0 || !m.statusErr || !strings.Contains(m.status, "no fix on record") {
		t.Errorf("copied=%v status=%q", clip.copied, m.status)
	}
}

func TestCopyFailureIsAnError(t *testing.T) {
	m := setup(t, newFakeStore(legacyNotice()), &fakeClipboard{err: errors.New("no display")})
	m, _ = press(t, m, "y")
	if !m.statusErr || m.status != "copy failed: no display" {
		t.Errorf("status = %q (err=%v)", m.status, m.statusErr)
	}
}

func TestEnterTogglesTheDetailPane(t *testing.T) {
	m := setup(t, newFakeStore(legacyNotice()), nil)

	if view := m.View(); !strings.Contains(view, "camp dungeon migrate") || !strings.Contains(view, legacyID) {
		t.Fatalf("detail pane is not open by default:\n%s", view)
	}
	m, _ = press(t, m, "enter")
	if view := m.View(); strings.Contains(view, "camp dungeon migrate") {
		t.Errorf("enter did not hide the detail pane:\n%s", view)
	}
	m, _ = press(t, m, "enter")
	if !strings.Contains(m.View(), "camp dungeon migrate") {
		t.Error("a second enter did not show the detail pane again")
	}
}

func TestHelpOverlayOpensAndClosesWithoutQuitting(t *testing.T) {
	m := setup(t, newFakeStore(legacyNotice()), nil)

	m, _ = press(t, m, "?")
	if !strings.Contains(m.View(), "Notice keys") {
		t.Fatal("? did not open the key help")
	}
	m, cmd := press(t, m, "esc")
	if m.showHelp || isQuit(cmd) {
		t.Fatalf("esc in help: showHelp=%v quit=%v", m.showHelp, isQuit(cmd))
	}
	m, cmd = press(t, m, "d")
	if cmd == nil {
		t.Error("keys stopped working after closing help")
	}
}

func TestQuitKeys(t *testing.T) {
	for _, key := range []string{"q", "esc", "ctrl+c"} {
		m := setup(t, newFakeStore(legacyNotice()), nil)
		m, cmd := press(t, m, key)
		if !isQuit(cmd) || m.View() != "" {
			t.Errorf("%s did not quit cleanly", key)
		}
	}
}

func TestBusyIgnoresActions(t *testing.T) {
	store := newFakeStore(legacyNotice(), linksNotice())
	m := setup(t, store, nil)

	m, _ = press(t, m, "d")
	m, cmd := press(t, m, "j")
	if cmd != nil || m.cursor != 0 {
		t.Errorf("j while busy moved the cursor or started work")
	}
	m, cmd = press(t, m, "d")
	if cmd != nil {
		t.Error("a second d while busy started another write")
	}
}

func TestQuitWhileBusyWaitsForTheWriteToLand(t *testing.T) {
	for _, key := range []string{"q", "esc", "ctrl+c"} {
		t.Run(key, func(t *testing.T) {
			store := newFakeStore(legacyNotice(), linksNotice())
			m := setup(t, store, nil)

			m, write := press(t, m, "d")
			if write == nil {
				t.Fatal("d did not start a write")
			}
			m, cmd := press(t, m, key)
			if isQuit(cmd) || m.quitting {
				t.Fatalf("%s quit before the dismissal reported", key)
			}
			if !strings.Contains(m.status, "finishing") {
				t.Errorf("status while the quit waits = %q", m.status)
			}

			next, cmd := m.Update(write())
			m = next.(Model)
			if !isQuit(cmd) {
				t.Fatal("the quit did not follow once the dismissal landed")
			}
			if got := m.Changes(); len(got) != 1 || got[0] != (Change{ID: legacyID, Dismissed: true}) {
				t.Errorf("exit report source Changes() = %+v, want the dismissal", got)
			}
			if len(m.Unsettled()) != 0 {
				t.Errorf("a landed write was reported unsettled: %+v", m.Unsettled())
			}
			if len(store.writes) != 1 {
				t.Errorf("writes = %v", store.writes)
			}
		})
	}
}

func TestSecondCtrlCForcesQuitAndNamesTheUnsettledWrite(t *testing.T) {
	m := setup(t, newFakeStore(legacyNotice()), nil)

	m, _ = press(t, m, "d")
	m, cmd := press(t, m, "ctrl+c")
	if isQuit(cmd) {
		t.Fatal("the first ctrl+c while busy quit at once")
	}
	m, cmd = press(t, m, "ctrl+c")
	if !isQuit(cmd) {
		t.Fatal("a second ctrl+c did not force the quit")
	}
	if got := m.Unsettled(); len(got) != 1 || got[0] != (Change{ID: legacyID, Dismissed: true}) {
		t.Errorf("Unsettled() = %+v, want the in-flight dismissal", got)
	}
	if len(m.Changes()) != 0 {
		t.Errorf("an unconfirmed write was reported as done: %+v", m.Changes())
	}
}

func TestViewFitsTheTerminal(t *testing.T) {
	store := newFakeStore(legacyNotice(), linksNotice(), rootNotice(rootAID))
	for i := 0; i < 20; i++ {
		store.dismissals.Dismiss("artifact-manifest-drift:data/"+string(rune('a'+i)), dismissedAt.Add(time.Duration(i)*time.Minute))
	}
	m := setup(t, store, nil)
	m = update(t, m, teaSize(60, 14))

	for i := 0; i < 25; i++ {
		lines := strings.Split(m.View(), "\n")
		if len(lines) > 14 {
			t.Fatalf("view has %d rows at cursor %d, terminal has 14", len(lines), m.cursor)
		}
		if !strings.Contains(lines[1], "Notices") {
			t.Fatalf("title scrolled off at cursor %d:\n%s", m.cursor, m.View())
		}
		m, _ = press(t, m, "j")
	}
}

func TestDismissedRowShowsMessageWhileStillDetected(t *testing.T) {
	store := newFakeStore(legacyNotice())
	store.dismissals.Dismiss(legacyID, dismissedAt)
	store.dismissals.Dismiss(rootAID, dismissedAt.Add(time.Hour))
	m := setup(t, store, nil)

	view := m.View()
	if !strings.Contains(view, "this camp uses the visible dungeon/ layout") {
		t.Errorf("a dismissed notice still detected should read as its message:\n%s", view)
	}
	if !strings.Contains(view, "data/a: "+notice.Summary(rootAID)) {
		t.Errorf("a dismissal with no message should read as its subject and summary:\n%s", view)
	}
}

func TestDismissalOfAnUndeclaredRootSaysSo(t *testing.T) {
	gone := notice.SubjectID(notice.KindNeverSynced, "data/gone")
	store := newFakeStore()
	store.dismissals.Dismiss(gone, dismissedAt)
	m := setup(t, store, nil)

	view := m.View()
	if !strings.Contains(view, "root no longer declared") || !strings.Contains(view, gone) {
		t.Errorf("a dismissal for a root that is gone should say so and keep its id:\n%s", view)
	}
}

func TestLongStatusWrapsInsteadOfClipping(t *testing.T) {
	m := setup(t, newFakeStore(rootNotice(rootAID)), nil)
	m = update(t, m, teaSize(60, 30))
	m, _ = press(t, m, "y")

	view := m.View()
	if !strings.Contains(view, "run it on another machine") || !strings.Contains(view, "copied camp sync") {
		t.Errorf("a long status lost its tail at 60 columns:\n%s", view)
	}
}

func TestDismissStatusNamesTheSubject(t *testing.T) {
	store := newFakeStore(rootNotice(rootAID))
	m := setup(t, store, nil)
	m = act(t, m, "d")
	if !strings.HasPrefix(m.status, "dismissed "+rootAID+" (data/a)") {
		t.Errorf("status = %q", m.status)
	}
}
