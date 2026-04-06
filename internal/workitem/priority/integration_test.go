package priority

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

func makeItem(key string, wfType workitem.WorkflowType, title string, sortTS time.Time) workitem.WorkItem {
	return workitem.WorkItem{
		Key:           key,
		WorkflowType:  wfType,
		Title:         title,
		RelativePath:  strings.TrimPrefix(key, string(wfType)+":"),
		SortTimestamp: sortTS,
		CreatedAt:     sortTS,
	}
}

func validKeysFrom(items []workitem.WorkItem) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		m[item.Key] = true
	}
	return m
}

func TestFilterSafety_TypeFilterPreservesPriorities(t *testing.T) {
	now := time.Now()
	items := []workitem.WorkItem{
		makeItem("intent:a", workitem.WorkflowTypeIntent, "Intent A", now),
		makeItem("design:b", workitem.WorkflowTypeDesign, "Design B", now),
		makeItem("intent:c", workitem.WorkflowTypeIntent, "Intent C", now),
	}

	store := NewStore()
	Set(store, "intent:a", High)
	Set(store, "design:b", Medium)

	// Prune against full set — nothing should be removed.
	if Prune(store, validKeysFrom(items)) {
		t.Error("prune should not remove anything when all keys are valid")
	}

	// Simulate --type design filter (view-only).
	_ = workitem.Filter(items, []string{"design"}, nil, "")

	// Store must still have both priorities.
	if len(store.ManualPriorities) != 2 {
		t.Fatalf("store has %d entries, want 2", len(store.ManualPriorities))
	}
	if store.ManualPriorities["intent:a"].Priority != High {
		t.Error("intent:a priority should be high")
	}
	if store.ManualPriorities["design:b"].Priority != Medium {
		t.Error("design:b priority should be medium")
	}
}

func TestFilterSafety_QueryFilterPreservesPriorities(t *testing.T) {
	now := time.Now()
	items := []workitem.WorkItem{
		makeItem("intent:auth-svc", workitem.WorkflowTypeIntent, "auth service", now),
		makeItem("design:pay-api", workitem.WorkflowTypeDesign, "payment api", now),
		makeItem("intent:auth-mid", workitem.WorkflowTypeIntent, "auth middleware", now),
		makeItem("explore:deploy", workitem.WorkflowTypeExplore, "deploy pipeline", now),
	}

	store := NewStore()
	Set(store, "intent:auth-svc", High)
	Set(store, "design:pay-api", Low)
	Set(store, "explore:deploy", Medium)

	Prune(store, validKeysFrom(items))

	// Simulate --query auth filter.
	_ = workitem.Filter(items, nil, nil, "auth")

	if len(store.ManualPriorities) != 3 {
		t.Fatalf("store has %d entries, want 3", len(store.ManualPriorities))
	}
}

func TestFilterSafety_LimitPreservesPriorities(t *testing.T) {
	now := time.Now()
	items := make([]workitem.WorkItem, 5)
	for i := range items {
		items[i] = makeItem("intent:"+string(rune('a'+i)), workitem.WorkflowTypeIntent, "", now.Add(-time.Duration(i)*time.Hour))
	}

	store := NewStore()
	Set(store, items[0].Key, High)
	Set(store, items[2].Key, Medium)
	Set(store, items[4].Key, Low)

	Prune(store, validKeysFrom(items))

	// Simulate --limit 1.
	limited := items[:1]
	_ = limited

	if len(store.ManualPriorities) != 3 {
		t.Fatalf("store has %d entries, want 3", len(store.ManualPriorities))
	}
}

func TestPrune_StaleItemIsRemoved(t *testing.T) {
	store := NewStore()
	Set(store, "intent:a", High)
	Set(store, "design:b", Medium)
	Set(store, "explore:c", Low)

	// explore:c no longer exists in discovery.
	validKeys := map[string]bool{"intent:a": true, "design:b": true}
	changed := Prune(store, validKeys)
	if !changed {
		t.Error("prune should report change when stale key removed")
	}
	if _, ok := store.ManualPriorities["explore:c"]; ok {
		t.Error("explore:c should have been pruned")
	}
	if len(store.ManualPriorities) != 2 {
		t.Errorf("store has %d entries, want 2", len(store.ManualPriorities))
	}
}

func TestPrune_StaleRemoval_SaveOrDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workitems.json")

	store := NewStore()
	Set(store, "gone:item", High)
	if err := Save(path, store); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	Prune(loaded, map[string]bool{}) // no valid keys — everything is stale
	if err := SaveOrDelete(path, loaded); err != nil {
		t.Fatal(err)
	}

	// File should be deleted since store is now empty.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ManualPriorities) != 0 {
		t.Error("store should be empty after pruning all entries")
	}
}

func TestPipeline_FullSequence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workitems.json")
	now := time.Now()

	// Step 1: Create items.
	items := []workitem.WorkItem{
		makeItem("intent:ship", workitem.WorkflowTypeIntent, "Ship Feature", now),
		makeItem("design:api", workitem.WorkflowTypeDesign, "API Design", now.Add(-time.Hour)),
		makeItem("intent:auth", workitem.WorkflowTypeIntent, "Auth Work", now.Add(-2*time.Hour)),
		makeItem("explore:perf", workitem.WorkflowTypeExplore, "Perf Research", now.Add(-3*time.Hour)),
		makeItem("festival:launch", workitem.WorkflowTypeFestival, "Launch Fest", now.Add(-4*time.Hour)),
	}

	// Step 2: Set priorities on 2 items and save.
	store := NewStore()
	Set(store, "design:api", High)
	Set(store, "explore:perf", Medium)
	if err := Save(path, store); err != nil {
		t.Fatal(err)
	}

	// Step 3: Simulate pipeline — Load.
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	// Step 4: Prune (no-op since all items exist).
	changed := Prune(store, validKeysFrom(items))
	if changed {
		t.Error("prune should be no-op when all keys are valid")
	}

	// Step 5: Apply overlay and sort.
	items = Apply(store, items)
	workitem.Sort(items)

	// Verify sort order: high-priority first, then medium, then rest by recency.
	if items[0].Key != "design:api" {
		t.Errorf("items[0] = %q, want design:api (high priority)", items[0].Key)
	}
	if items[1].Key != "explore:perf" {
		t.Errorf("items[1] = %q, want explore:perf (medium priority)", items[1].Key)
	}

	// Step 6: Filter by type intent (filters out the prioritized items from view).
	filtered := workitem.Filter(items, []string{"intent"}, nil, "")
	if len(filtered) != 2 {
		t.Fatalf("filtered to %d items, want 2 intents", len(filtered))
	}

	// Step 7: Apply limit.
	if len(filtered) > 1 {
		filtered = filtered[:1]
	}

	// Verify: store on disk still has both priorities.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ManualPriorities) != 2 {
		t.Errorf("store has %d entries, want 2 (filter must not erase priorities)", len(reloaded.ManualPriorities))
	}
	if reloaded.ManualPriorities["design:api"].Priority != High {
		t.Error("design:api priority should still be high")
	}
	if reloaded.ManualPriorities["explore:perf"].Priority != Medium {
		t.Error("explore:perf priority should still be medium")
	}
}
