//go:build container_fs

package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
)

const (
	actionIntentID = "ship-notes-20260727-123326"
	otherIntentID  = "hold-notes-20260727-123327"
)

func TestDocumentActionsFS_PinPreservesUnrelatedRowsAndRefusesRawSet(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	for _, name := range []string{"docs", "src", "notes"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	initial := []byte(`[
  {"name":"kept","path":"docs","created_at":"2020-01-02T03:04:05Z","custom_note":"stay","serial":9007199254740993},
  {"name":"other","path":"src","created_at":"2021-05-06T07:08:09Z","extra":true}
]
`)
	writeRel(t, root, "settings/pins.json", initial)
	keptBefore := pinRow(t, decodeJSON(t, initial), "kept")
	otherBefore := pinRow(t, decodeJSON(t, initial), "other")

	pinned, err := svc.Action(ctx, "settings/pins.json", "pin", RevisionOf(initial), []byte(`{"name":"fresh","path":"notes"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !pinned.Applied || pinned.Path != "settings/pins.json" {
		t.Fatalf("pin snapshot path=%s applied=%t", pinned.Path, pinned.Applied)
	}
	afterPin := readCamp(t, root, "settings/pins.json")
	pinnedDoc := decodeJSON(t, afterPin)
	keptAfterPin := pinRow(t, pinnedDoc, "kept")
	assertJSONNumber(t, keptAfterPin, "serial", "9007199254740993")
	if !reflect.DeepEqual(keptAfterPin, keptBefore) {
		t.Fatalf("kept pin changed: %#v", keptAfterPin)
	}
	if !reflect.DeepEqual(pinRow(t, pinnedDoc, "other"), otherBefore) {
		t.Fatalf("other pin changed: %#v", pinRow(t, pinnedDoc, "other"))
	}
	fresh := pinRow(t, pinnedDoc, "fresh")
	if fresh["path"] != "notes" || fresh["created_at"] == "" {
		t.Fatalf("fresh pin = %#v", fresh)
	}

	unpinned, err := svc.Action(ctx, "settings/pins.json", "unpin", RevisionOf(afterPin), []byte(`{"name":"other"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !unpinned.Applied {
		t.Fatal("unpin was not applied")
	}
	afterUnpin := readCamp(t, root, "settings/pins.json")
	unpinDoc := decodeJSON(t, afterUnpin)
	keptAfterUnpin := pinRow(t, unpinDoc, "kept")
	assertJSONNumber(t, keptAfterUnpin, "serial", "9007199254740993")
	if !reflect.DeepEqual(keptAfterUnpin, keptBefore) {
		t.Fatalf("kept pin changed on unpin: %#v", keptAfterUnpin)
	}
	if _, ok := findPin(unpinDoc, "other"); ok {
		t.Fatal("unpinned row remained")
	}
	if pinRow(t, unpinDoc, "fresh")["path"] != "notes" {
		t.Fatal("fresh pin was dropped")
	}

	current := readCamp(t, root, "settings/pins.json")
	rev := RevisionOf(current)
	refusals := []struct {
		name   string
		action string
		body   string
	}{
		{name: "duplicate", action: "pin", body: `{"name":"kept","path":"src"}`},
		{name: "unknown action", action: "pin-everything", body: `{"name":"n","path":"notes"}`},
		{name: "invalid body", action: "pin", body: `{"name":`},
	}
	for _, refusal := range refusals {
		snap, err := svc.Action(ctx, "settings/pins.json", refusal.action, rev, []byte(refusal.body))
		assertUnapplied(t, snap, err)
		assertFile(t, root, "settings/pins.json", current)
	}

	external := []byte("[\n  {\"name\":\"external\",\"path\":\"docs\",\"created_at\":\"2019-01-01T00:00:00Z\",\"marker\":\"external\"}\n]\n")
	writeRel(t, root, "settings/pins.json", external)
	snap, err := svc.Action(ctx, "settings/pins.json", "pin", rev, []byte(`{"name":"fresh","path":"notes"}`))
	requireConflict(t, snapErr(snap, err))
	assertFile(t, root, "settings/pins.json", external)

	replaced, err := svc.Set(ctx, "settings/pins.json", RevisionOf(external), []byte("[]\n"))
	assertUnapplied(t, replaced, err)
	assertFile(t, root, "settings/pins.json", external)
}

func TestDocumentActionsFS_WorkitemPriorityAndGroupPreserveUnknownFields(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	stageActiveIntent(t, root, actionIntentID, "Ship notes")
	stageActiveIntent(t, root, otherIntentID, "Hold notes")
	intentBytes := readCamp(t, root, "intents/active/"+actionIntentID+".md")
	target, err := selector.Resolve(ctx, root, actionIntentID, selector.ResolveOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	other, err := selector.Resolve(ctx, root, otherIntentID, selector.ResolveOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{
		"version":      1,
		"custom_store": map[string]any{"kept": true, "n": 2.0},
		"manual_priorities": map[string]any{
			other.Key: map[string]any{
				"priority":   "low",
				"updated_at": "2020-01-02T03:04:05Z",
				"note":       "stay",
			},
		},
		"attention": map[string]any{
			other.Key: map[string]any{
				"stage":      "parked",
				"group":      "alpha",
				"updated_at": "2021-06-07T08:09:10Z",
				"hint":       "keep",
			},
		},
	}
	raw, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	writeRel(t, root, "settings/workitems.json", raw)
	original := decodeJSON(t, raw)
	otherPriority := jsonField(t, jsonField(t, original, "manual_priorities"), other.Key)
	otherAttention := jsonField(t, jsonField(t, original, "attention"), other.Key)
	customStore := jsonField(t, original, "custom_store")

	body := []byte(`{"selector":"` + actionIntentID + `","priority":"high"}`)
	setPriority, err := svc.Action(ctx, "settings/workitems.json", "workitem-priority", RevisionOf(raw), body)
	if err != nil {
		t.Fatal(err)
	}
	if !setPriority.Applied {
		t.Fatal("priority action was not applied")
	}
	afterPriority := decodeJSON(t, readCamp(t, root, "settings/workitems.json"))
	assertSameJSON(t, "custom store", jsonField(t, afterPriority, "custom_store"), customStore)
	assertSameJSON(t, "unrelated priority", jsonField(t, jsonField(t, afterPriority, "manual_priorities"), other.Key), otherPriority)
	assertSameJSON(t, "unrelated attention", jsonField(t, jsonField(t, afterPriority, "attention"), other.Key), otherAttention)
	if got := jsonField(t, jsonField(t, jsonField(t, afterPriority, "manual_priorities"), target.Key), "priority"); got != "high" {
		t.Fatalf("target priority = %#v", got)
	}

	current := readCamp(t, root, "settings/workitems.json")
	rejected, err := svc.Action(ctx, "settings/workitems.json", "workitem-group", RevisionOf(current), []byte(`{"selector":"`+actionIntentID+`","group":"beta"}`))
	assertUnapplied(t, rejected, err)
	if err == nil {
		t.Fatal("grouping an intent was accepted")
	}
	assertFile(t, root, "settings/workitems.json", current)

	stageDesignGroup(t, root)
	design, err := selector.Resolve(ctx, root, "workflow/design/docs-group", selector.ResolveOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	grouped, err := svc.Action(ctx, "settings/workitems.json", "workitem-group", RevisionOf(current), []byte(`{"selector":"workflow/design/docs-group","group":"beta"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !grouped.Applied {
		t.Fatal("group action was not applied")
	}
	afterGroup := decodeJSON(t, readCamp(t, root, "settings/workitems.json"))
	assertSameJSON(t, "custom store after group", jsonField(t, afterGroup, "custom_store"), customStore)
	assertSameJSON(t, "unrelated priority after group", jsonField(t, jsonField(t, afterGroup, "manual_priorities"), other.Key), otherPriority)
	assertSameJSON(t, "unrelated attention after group", jsonField(t, jsonField(t, afterGroup, "attention"), other.Key), otherAttention)
	assertSameJSON(t, "earlier priority", jsonField(t, jsonField(t, afterGroup, "manual_priorities"), target.Key), jsonField(t, jsonField(t, afterPriority, "manual_priorities"), target.Key))
	if got := jsonField(t, jsonField(t, jsonField(t, afterGroup, "attention"), design.Key), "group"); got != "beta" {
		t.Fatalf("design group = %#v", got)
	}

	current = readCamp(t, root, "settings/workitems.json")
	clearedGroup, err := svc.Action(ctx, "settings/workitems.json", "workitem-group", RevisionOf(current), []byte(`{"selector":"workflow/design/docs-group","group":""}`))
	if err != nil {
		t.Fatal(err)
	}
	if !clearedGroup.Applied {
		t.Fatal("group clear was not applied")
	}
	afterGroupClear := decodeJSON(t, readCamp(t, root, "settings/workitems.json"))
	attention, _ := jsonField(t, afterGroupClear, "attention").(map[string]any)
	if entry, ok := attention[design.Key].(map[string]any); ok && entry["group"] != nil && entry["group"] != "" {
		t.Fatalf("cleared group = %#v", entry["group"])
	}
	assertSameJSON(t, "unrelated attention after group clear", attention[other.Key], otherAttention)
	assertSameJSON(t, "custom store after group clear", jsonField(t, afterGroupClear, "custom_store"), customStore)
	assertSameJSON(t, "earlier priority after group clear", jsonField(t, jsonField(t, afterGroupClear, "manual_priorities"), target.Key), jsonField(t, jsonField(t, afterPriority, "manual_priorities"), target.Key))
	assertFile(t, root, "intents/active/"+actionIntentID+".md", intentBytes)

	current = readCamp(t, root, "settings/workitems.json")
	cleared, err := svc.Action(ctx, "settings/workitems.json", "workitem-priority", RevisionOf(current), []byte(`{"selector":"`+actionIntentID+`","priority":"clear"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !cleared.Applied {
		t.Fatal("priority clear was not applied")
	}
	afterClear := decodeJSON(t, readCamp(t, root, "settings/workitems.json"))
	priorities, _ := jsonField(t, afterClear, "manual_priorities").(map[string]any)
	if _, ok := priorities[target.Key]; ok {
		t.Fatalf("cleared priority remained: %#v", priorities[target.Key])
	}
	assertSameJSON(t, "unrelated priority after clear", priorities[other.Key], otherPriority)
	assertSameJSON(t, "custom store after clear", jsonField(t, afterClear, "custom_store"), customStore)
	assertSameJSON(t, "unrelated attention after clear", jsonField(t, jsonField(t, afterClear, "attention"), other.Key), otherAttention)

	final := readCamp(t, root, "settings/workitems.json")
	replaced, err := svc.Set(ctx, "settings/workitems.json", RevisionOf(final), []byte("{\"version\":1}\n"))
	assertUnapplied(t, replaced, err)
	assertFile(t, root, "settings/workitems.json", final)
}

func TestDocumentActionsFS_CanceledActionLeavesTreeUnchanged(t *testing.T) {
	svc, root := newDocumentFixture(t)
	stageActiveIntent(t, root, actionIntentID, "Ship notes")
	writeRel(t, root, "settings/pins.json", []byte("[]\n"))
	writeRel(t, root, "settings/workitems.json", []byte("{\"version\":1,\"manual_priorities\":{},\"attention\":{}}\n"))
	before := stampCampaign(t, root)
	pins := readCamp(t, root, "settings/pins.json")
	work := readCamp(t, root, "settings/workitems.json")
	dead, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := svc.Action(dead, "settings/pins.json", "pin", RevisionOf(pins), []byte(`{"name":"fresh","path":"notes"}`))
	op := mustOp(t, err)
	if op.Code != CodeCancelled || op.Applied {
		t.Fatalf("cancelled pin = %+v", op)
	}
	assertStamp(t, root, before)

	_, err = svc.Action(dead, "settings/workitems.json", "workitem-priority", RevisionOf(work), []byte(`{"selector":"`+actionIntentID+`","priority":"high"}`))
	op = mustOp(t, err)
	if op.Code != CodeCancelled || op.Applied {
		t.Fatalf("cancelled priority = %+v", op)
	}
	assertStamp(t, root, before)
}

func stageActiveIntent(t *testing.T, root, id, title string) {
	t.Helper()
	body := "---\nid: " + id + "\ntitle: " + title + "\nstatus: active\ncreated_at: 2026-07-27T12:33:26Z\ntype: bug\n---\n\n# " + title + "\n"
	writeRel(t, root, "intents/active/"+id+".md", []byte(body))
}

func stageDesignGroup(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "workflow", "design", "docs-group")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := "version: " + workitem.WorkitemSchemaVersion + "\nkind: " + workitem.MetadataKind + "\nid: design-docs-group-fixed\ntype: design\ntitle: Docs group\nref: WI-aabb11\n"
	if err := os.WriteFile(filepath.Join(dir, workitem.MetadataFilename), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Docs group\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func decodeJSON(t *testing.T, body []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("decode json: %v\n%s", err, body)
	}
	return doc
}

func assertJSONNumber(t *testing.T, row map[string]any, key, digits string) {
	t.Helper()
	num, ok := row[key].(json.Number)
	if !ok || num.String() != digits {
		t.Fatalf("%s = %#v, want json.Number %s", key, row[key], digits)
	}
}

func pinRow(t *testing.T, doc any, name string) map[string]any {
	t.Helper()
	row, ok := findPin(doc, name)
	if !ok {
		t.Fatalf("pin %s missing", name)
	}
	return row
}

func findPin(doc any, name string) (map[string]any, bool) {
	rows, ok := doc.([]any)
	if !ok {
		return nil, false
	}
	for _, row := range rows {
		mapped, ok := row.(map[string]any)
		if ok && mapped["name"] == name {
			return mapped, true
		}
	}
	return nil, false
}

func jsonField(t *testing.T, doc any, key string) any {
	t.Helper()
	mapped, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("json field %s parent is %T", key, doc)
	}
	value, ok := mapped[key]
	if !ok {
		t.Fatalf("json field %s missing", key)
	}
	return value
}

func assertSameJSON(t *testing.T, label string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", label, got, want)
	}
}

func snapErr(snap Snapshot, err error) error {
	if err != nil {
		return err
	}
	if snap.Applied {
		return &OpError{Code: "applied", Message: "action applied", Applied: true}
	}
	return &OpError{Code: "missing", Message: "action returned no error"}
}
