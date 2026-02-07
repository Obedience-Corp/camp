package feedback

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashObservation_Deterministic(t *testing.T) {
	obs := &Observation{
		Criteria:    "UX Issues",
		Observation: "Button is confusing",
		Suggestion:  "Rename it",
	}

	hash1 := HashObservation(obs)
	hash2 := HashObservation(obs)

	if hash1 != hash2 {
		t.Error("hash should be deterministic")
	}

	if len(hash1) != 64 {
		t.Errorf("expected 64 char hex SHA-256, got %d chars", len(hash1))
	}
}

func TestHashObservation_ExcludesMetadata(t *testing.T) {
	obs1 := &Observation{
		Criteria:    "UX",
		Observation: "Issue",
		Suggestion:  "Fix",
		Timestamp:   "2026-01-01T00:00:00Z",
		Severity:    "high",
	}

	obs2 := &Observation{
		Criteria:    "UX",
		Observation: "Issue",
		Suggestion:  "Fix",
		Timestamp:   "2026-02-02T00:00:00Z",
		Severity:    "low",
	}

	if HashObservation(obs1) != HashObservation(obs2) {
		t.Error("hash should be the same when only timestamp/severity differ")
	}
}

func TestHashObservation_DifferentContent(t *testing.T) {
	obs1 := &Observation{
		Criteria:    "UX",
		Observation: "Issue A",
	}

	obs2 := &Observation{
		Criteria:    "UX",
		Observation: "Issue B",
	}

	if HashObservation(obs1) == HashObservation(obs2) {
		t.Error("different observations should produce different hashes")
	}
}

func TestTracker_LoadSave(t *testing.T) {
	dir := t.TempDir()
	tracker := NewTracker()

	// Load non-existent should return nil
	tracking, err := tracker.Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tracking != nil {
		t.Error("expected nil tracking for non-existent file")
	}

	// Save tracking
	gt := &GatheredTracking{
		Version: 1,
		Hashes:  []string{"abc123", "def456"},
	}
	if err := tracker.Save(dir, gt); err != nil {
		t.Fatalf("unexpected error saving: %v", err)
	}

	// Verify file exists
	path := filepath.Join(dir, ".fest", "gathered_feedback.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("tracking file not created: %v", err)
	}

	// Load it back
	loaded, err := tracker.Load(dir)
	if err != nil {
		t.Fatalf("unexpected error loading: %v", err)
	}
	if loaded.Version != 1 {
		t.Errorf("expected version 1, got %d", loaded.Version)
	}
	if len(loaded.Hashes) != 2 {
		t.Errorf("expected 2 hashes, got %d", len(loaded.Hashes))
	}
}

func TestTracker_FilterNew_AllNew(t *testing.T) {
	dir := t.TempDir()
	tracker := NewTracker()

	obs := []Observation{
		{Criteria: "UX", Observation: "Issue 1"},
		{Criteria: "UX", Observation: "Issue 2"},
	}

	newObs, err := tracker.FilterNew(dir, obs, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(newObs) != 2 {
		t.Errorf("expected 2 new observations, got %d", len(newObs))
	}
}

func TestTracker_FilterNew_SomeGathered(t *testing.T) {
	dir := t.TempDir()
	tracker := NewTracker()

	obs := []Observation{
		{Criteria: "UX", Observation: "Issue 1"},
		{Criteria: "UX", Observation: "Issue 2"},
	}

	// Record first observation as gathered
	if err := tracker.RecordGathered(dir, obs[:1]); err != nil {
		t.Fatalf("unexpected error recording: %v", err)
	}

	// Filter should only return the second observation
	newObs, err := tracker.FilterNew(dir, obs, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(newObs) != 1 {
		t.Errorf("expected 1 new observation, got %d", len(newObs))
	}
	if newObs[0].Observation != "Issue 2" {
		t.Errorf("expected Issue 2, got %s", newObs[0].Observation)
	}
}

func TestTracker_FilterNew_AllGathered(t *testing.T) {
	dir := t.TempDir()
	tracker := NewTracker()

	obs := []Observation{
		{Criteria: "UX", Observation: "Issue 1"},
	}

	if err := tracker.RecordGathered(dir, obs); err != nil {
		t.Fatalf("unexpected error recording: %v", err)
	}

	newObs, err := tracker.FilterNew(dir, obs, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(newObs) != 0 {
		t.Errorf("expected 0 new observations, got %d", len(newObs))
	}
}

func TestTracker_FilterNew_ForceIgnoresTracking(t *testing.T) {
	dir := t.TempDir()
	tracker := NewTracker()

	obs := []Observation{
		{Criteria: "UX", Observation: "Issue 1"},
	}

	if err := tracker.RecordGathered(dir, obs); err != nil {
		t.Fatalf("unexpected error recording: %v", err)
	}

	// With force=true, should return all observations
	newObs, err := tracker.FilterNew(dir, obs, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(newObs) != 1 {
		t.Errorf("expected 1 observation with force, got %d", len(newObs))
	}
}

func TestTracker_RecordGathered_Appends(t *testing.T) {
	dir := t.TempDir()
	tracker := NewTracker()

	batch1 := []Observation{{Criteria: "UX", Observation: "Issue 1"}}
	batch2 := []Observation{{Criteria: "UX", Observation: "Issue 2"}}

	if err := tracker.RecordGathered(dir, batch1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := tracker.RecordGathered(dir, batch2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tracking, err := tracker.Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tracking.Hashes) != 2 {
		t.Errorf("expected 2 hashes after two batches, got %d", len(tracking.Hashes))
	}
}
