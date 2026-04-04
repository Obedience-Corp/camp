package priority

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workitems.json")
	if err := os.WriteFile(path, []byte("{bad json}"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load of invalid JSON should return error")
	}
	if !strings.Contains(err.Error(), "parsing priority store") {
		t.Errorf("error should mention parsing, got: %s", err)
	}
}

func TestLoad_NonexistentFile(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("Load of nonexistent file returned error: %v", err)
	}
	if s == nil {
		t.Fatal("Load of nonexistent file returned nil store")
	}
	if s.Version != 1 {
		t.Errorf("Version = %d, want 1", s.Version)
	}
	if s.ManualPriorities == nil {
		t.Error("ManualPriorities should be initialized, got nil")
	}
	if len(s.ManualPriorities) != 0 {
		t.Errorf("ManualPriorities should be empty, got %d entries", len(s.ManualPriorities))
	}
}

func TestLoad_ValidJSON(t *testing.T) {
	ts := time.Date(2026, 3, 31, 20, 17, 12, 0, time.UTC)
	store := &Store{
		Version: 1,
		ManualPriorities: map[string]PriorityEntry{
			"intent:ship-camp": {Priority: High, UpdatedAt: ts},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "workitems.json")
	data, _ := json.MarshalIndent(store, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.Version != 1 {
		t.Errorf("Version = %d, want 1", loaded.Version)
	}
	entry, ok := loaded.ManualPriorities["intent:ship-camp"]
	if !ok {
		t.Fatal("expected entry for intent:ship-camp")
	}
	if entry.Priority != High {
		t.Errorf("Priority = %q, want %q", entry.Priority, High)
	}
	if !entry.UpdatedAt.Equal(ts) {
		t.Errorf("UpdatedAt = %v, want %v", entry.UpdatedAt, ts)
	}
}

func TestLoad_NilMapInitialized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workitems.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"manual_priorities":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.ManualPriorities == nil {
		t.Error("ManualPriorities should be initialized after loading null map")
	}
}

func TestSave_Load_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workitems.json")

	original := NewStore()
	Set(original, "key-a", High)
	Set(original, "key-b", Low)

	if err := Save(path, original); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(loaded.ManualPriorities) != 2 {
		t.Fatalf("got %d entries, want 2", len(loaded.ManualPriorities))
	}
	if loaded.ManualPriorities["key-a"].Priority != High {
		t.Errorf("key-a priority = %q, want %q", loaded.ManualPriorities["key-a"].Priority, High)
	}
	if loaded.ManualPriorities["key-b"].Priority != Low {
		t.Errorf("key-b priority = %q, want %q", loaded.ManualPriorities["key-b"].Priority, Low)
	}
}

func TestSave_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "workitems.json")
	if err := Save(path, NewStore()); err != nil {
		t.Fatalf("Save should create parent dirs, got error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should exist after Save: %v", err)
	}
}

func TestSave_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workitems.json")
	if err := Save(path, NewStore()); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("file should exist at target path after Save")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temp file %q was not cleaned up", e.Name())
		}
	}
}

func TestSet_NewEntry(t *testing.T) {
	s := NewStore()
	before := time.Now().UTC().Add(-time.Second)
	Set(s, "item-1", Medium)
	after := time.Now().UTC().Add(time.Second)

	entry, ok := s.ManualPriorities["item-1"]
	if !ok {
		t.Fatal("entry should exist after Set")
	}
	if entry.Priority != Medium {
		t.Errorf("Priority = %q, want %q", entry.Priority, Medium)
	}
	if entry.UpdatedAt.Before(before) || entry.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt %v is not between %v and %v", entry.UpdatedAt, before, after)
	}
}

func TestSet_UpdateEntry(t *testing.T) {
	s := NewStore()
	Set(s, "item-1", Low)
	Set(s, "item-1", High)

	if len(s.ManualPriorities) != 1 {
		t.Errorf("expected 1 entry, got %d", len(s.ManualPriorities))
	}
	if s.ManualPriorities["item-1"].Priority != High {
		t.Errorf("Priority = %q, want %q", s.ManualPriorities["item-1"].Priority, High)
	}
}

func TestClear_ExistingEntry(t *testing.T) {
	s := NewStore()
	Set(s, "item-1", High)
	Clear(s, "item-1")
	if _, ok := s.ManualPriorities["item-1"]; ok {
		t.Error("entry should be removed after Clear")
	}
}

func TestClear_NonexistentKey(t *testing.T) {
	s := NewStore()
	Clear(s, "nonexistent") // should not panic
}

func TestPrune_RemovesStaleKeys(t *testing.T) {
	s := NewStore()
	Set(s, "a", High)
	Set(s, "b", Medium)
	Set(s, "c", Low)

	changed := Prune(s, map[string]bool{"a": true, "c": true})
	if !changed {
		t.Error("Prune should return true when entries are removed")
	}
	if _, ok := s.ManualPriorities["b"]; ok {
		t.Error("stale key 'b' should be pruned")
	}
	if len(s.ManualPriorities) != 2 {
		t.Errorf("expected 2 entries after prune, got %d", len(s.ManualPriorities))
	}
}

func TestPrune_KeepsAllValid(t *testing.T) {
	s := NewStore()
	Set(s, "a", High)
	Set(s, "b", Medium)

	changed := Prune(s, map[string]bool{"a": true, "b": true})
	if changed {
		t.Error("Prune should return false when nothing is removed")
	}
	if len(s.ManualPriorities) != 2 {
		t.Errorf("expected 2 entries, got %d", len(s.ManualPriorities))
	}
}

func TestPrune_EmptyStore(t *testing.T) {
	s := NewStore()
	changed := Prune(s, map[string]bool{"a": true})
	if changed {
		t.Error("Prune on empty store should return false")
	}
}

func TestPrune_AllStale(t *testing.T) {
	s := NewStore()
	Set(s, "a", High)
	Set(s, "b", Low)

	changed := Prune(s, map[string]bool{})
	if !changed {
		t.Error("Prune should return true when all entries are removed")
	}
	if len(s.ManualPriorities) != 0 {
		t.Errorf("expected 0 entries after pruning all, got %d", len(s.ManualPriorities))
	}
}

func TestSaveOrDelete_EmptyStoreDeletesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workitems.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveOrDelete(path, NewStore()); err != nil {
		t.Fatalf("SaveOrDelete returned error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should be deleted when store is empty")
	}
}

func TestSaveOrDelete_NonEmptyStoreSavesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workitems.json")

	s := NewStore()
	Set(s, "key", High)
	if err := SaveOrDelete(path, s); err != nil {
		t.Fatalf("SaveOrDelete returned error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("file should exist when store is non-empty")
	}
}

func TestSaveOrDelete_DeleteNonexistentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if err := SaveOrDelete(path, NewStore()); err != nil {
		t.Fatalf("SaveOrDelete on nonexistent file should succeed, got: %v", err)
	}
}

func TestIsEmpty(t *testing.T) {
	s := NewStore()
	if !IsEmpty(s) {
		t.Error("new store should be empty")
	}
	Set(s, "key", Low)
	if IsEmpty(s) {
		t.Error("store with entry should not be empty")
	}
}

func TestStorePath(t *testing.T) {
	got := StorePath("/home/user/campaign")
	want := filepath.Join("/home/user/campaign", ".campaign", "settings", "workitems.json")
	if got != want {
		t.Errorf("StorePath = %q, want %q", got, want)
	}
}

func TestApply_DecoratesMatchingItems(t *testing.T) {
	s := NewStore()
	Set(s, "intent:a", High)
	Set(s, "design:b", Medium)

	items := []workitem.WorkItem{
		{Key: "intent:a"},
		{Key: "design:b"},
		{Key: "explore:c"},
	}
	items = Apply(s, items)

	if items[0].ManualPriority != "high" {
		t.Errorf("intent:a ManualPriority = %q, want high", items[0].ManualPriority)
	}
	if items[1].ManualPriority != "medium" {
		t.Errorf("design:b ManualPriority = %q, want medium", items[1].ManualPriority)
	}
	if items[2].ManualPriority != "" {
		t.Errorf("explore:c ManualPriority = %q, want empty", items[2].ManualPriority)
	}
}

func TestApply_ClearsStaleValues(t *testing.T) {
	s := NewStore()
	Set(s, "intent:a", High)

	items := []workitem.WorkItem{
		{Key: "intent:a", ManualPriority: "high"},
		{Key: "design:b", ManualPriority: "medium"}, // stale: not in store
	}
	items = Apply(s, items)

	if items[0].ManualPriority != "high" {
		t.Errorf("intent:a ManualPriority = %q, want high", items[0].ManualPriority)
	}
	if items[1].ManualPriority != "" {
		t.Errorf("design:b ManualPriority = %q, want empty (cleared by Apply)", items[1].ManualPriority)
	}
}

func TestApply_EmptyStore(t *testing.T) {
	s := NewStore()
	items := []workitem.WorkItem{
		{Key: "intent:a", ManualPriority: "high"}, // stale value
		{Key: "design:b"},
	}
	items = Apply(s, items)

	if items[0].ManualPriority != "" {
		t.Errorf("intent:a ManualPriority = %q, want empty (empty store clears all)", items[0].ManualPriority)
	}
	if items[1].ManualPriority != "" {
		t.Errorf("design:b ManualPriority = %q, want empty", items[1].ManualPriority)
	}
}
