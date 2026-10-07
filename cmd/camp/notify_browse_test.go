package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/notice"
	tuinotify "github.com/Obedience-Corp/camp/internal/tui/notify"
)

func TestEmitNotifyJSON_EmptyListsAreArrays(t *testing.T) {
	var buf bytes.Buffer
	if err := emitNotifyJSON(&buf, "/camp", notice.Partition(nil, &notice.DismissalFile{}, nil)); err != nil {
		t.Fatalf("emit: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	for _, key := range []string{"live", "dismissed"} {
		if string(raw[key]) != "[]" {
			t.Errorf("%s = %s, want []", key, raw[key])
		}
	}
	if string(raw["schema_version"]) != `"notify/v1alpha1"` {
		t.Errorf("schema_version = %s", raw["schema_version"])
	}
}

func TestEmitNotifyJSON_Shape(t *testing.T) {
	at := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	rootID := notice.SubjectID(notice.KindNeverSynced, "data")
	inv := notice.Partition(
		[]notice.Notice{
			{ID: notice.StaleLinksID, Message: "1 workitem link points at a path that no longer exists", Command: "camp workitem doctor --fix"},
			{ID: notice.DungeonLegacyID, Message: "legacy layout", Command: "camp dungeon migrate"},
		},
		&notice.DismissalFile{Dismissed: map[string]time.Time{
			notice.DungeonLegacyID: at,
			rootID:                 at,
		}},
		map[string]string{rootID: "data"},
	)
	var buf bytes.Buffer
	if err := emitNotifyJSON(&buf, "/camp", inv); err != nil {
		t.Fatalf("emit: %v", err)
	}
	var got notifyPayload
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Live) != 1 || got.Live[0].Command != "camp workitem doctor --fix" || got.Live[0].Subject != "" {
		t.Errorf("live = %+v", got.Live)
	}
	if len(got.Dismissed) != 2 {
		t.Fatalf("dismissed = %+v", got.Dismissed)
	}
	unreported, legacy := got.Dismissed[1], got.Dismissed[0]
	if unreported.ID != rootID {
		unreported, legacy = legacy, unreported
	}
	if unreported.Subject != "data" || unreported.Summary != notice.Summary(rootID) || unreported.Message != "" {
		t.Errorf("unreported dismissal = %+v, want subject and summary with no message", unreported)
	}
	if legacy.Message != "legacy layout" || legacy.DismissedAt != "2026-10-06T12:00:00Z" {
		t.Errorf("legacy dismissal = %+v", legacy)
	}
	var raw struct {
		Live []map[string]json.RawMessage `json:"live"`
	}
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if _, ok := raw.Live[0]["subject"]; !ok {
		t.Errorf("live entries must always carry subject:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "Message") || strings.Contains(buf.String(), "dismissedAt") {
		t.Errorf("payload keys must be snake_case:\n%s", buf.String())
	}
}

func TestEmitNotifyJSON_KeepsPlaceholdersReadable(t *testing.T) {
	inv := notice.Partition([]notice.Notice{{ID: "artifact-roots-missing-locally", Message: "1 declared artifact root is not on this machine",
		Command: "camp sync --from <machine>   (dismiss: camp notify dismiss artifact-roots-missing-locally)"}}, &notice.DismissalFile{}, nil)
	var buf bytes.Buffer
	if err := emitNotifyJSON(&buf, "/camp", inv); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(buf.String(), `"command": "camp sync --from <machine>"`) {
		t.Errorf("command should be the bare fix with its placeholder unescaped:\n%s", buf.String())
	}
}

func TestRenderNotifyPlain(t *testing.T) {
	var empty bytes.Buffer
	renderNotifyPlain(&empty, notice.Partition(nil, &notice.DismissalFile{}, nil))
	if strings.TrimSpace(empty.String()) != "No notices" {
		t.Errorf("empty listing = %q", empty.String())
	}

	rootID := notice.SubjectID(notice.KindNeverSynced, "data/models")
	var buf bytes.Buffer
	renderNotifyPlain(&buf, notice.Partition(
		[]notice.Notice{
			{ID: notice.DungeonLegacyID, Message: "legacy layout", Command: "camp dungeon migrate"},
			{ID: rootID, Subject: "data/models", Message: "data/models has never synced", Command: "camp commit"},
		},
		&notice.DismissalFile{Dismissed: map[string]time.Time{"artifact-roots-missing-locally": time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}},
		nil,
	))
	out := buf.String()
	for _, want := range []string{
		"LIVE NOTICES (2)", "  " + notice.DungeonLegacyID + "\n", "    fix: camp dungeon migrate",
		"  " + rootID + "  data/models\n",
		"DISMISSED NOTICES (1)", "  2026-10-06  artifact-roots-missing-locally",
		"    " + notice.Summary("artifact-roots-missing-locally"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("listing lacks %q:\n%s", want, out)
		}
	}
}

func TestNotifyTUIRequested(t *testing.T) {
	defer func(json, plain bool) { notifyOpts.json, notifyOpts.plain = json, plain }(notifyOpts.json, notifyOpts.plain)

	notifyOpts.json, notifyOpts.plain = false, false
	if !notifyTUIRequested(true) || notifyTUIRequested(false) {
		t.Error("bare notify must open the browser on a terminal and only there")
	}
	notifyOpts.plain = true
	if notifyTUIRequested(true) {
		t.Error("--plain opened the browser")
	}
	notifyOpts.plain, notifyOpts.json = false, true
	if notifyTUIRequested(true) {
		t.Error("--json opened the browser")
	}
}

func TestReportNotifyChangesNamesTheUndo(t *testing.T) {
	var buf bytes.Buffer
	reportNotifyChanges(&buf, []tuinotify.Change{
		{ID: notice.DungeonLegacyID, Dismissed: true},
		{ID: notice.StaleLinksID, Dismissed: false},
	})
	out := buf.String()
	for _, want := range []string{
		"Dismissed dungeon-legacy-layout. Undo: camp notify restore dungeon-legacy-layout",
		"Restored workitem-links-stale. Undo: camp notify dismiss workitem-links-stale",
		"Recorded in .campaign/notices.yaml.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}

	var none bytes.Buffer
	reportNotifyChanges(&none, nil)
	if none.Len() != 0 {
		t.Errorf("a session that changed nothing printed %q", none.String())
	}
}
