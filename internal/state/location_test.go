package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadState_NoFile(t *testing.T) {
	tmpDir := t.TempDir()

	state, err := LoadState(context.Background(), tmpDir)
	require.NoError(t, err, "LoadState should not error when state file doesn't exist")
	assert.NotNil(t, state, "should return empty state, not nil")
	assert.Equal(t, "", state.LastLocation, "empty state should have empty location")
}

func TestSaveAndLoadState(t *testing.T) {
	tmpDir := t.TempDir()
	campaignDir := filepath.Join(tmpDir, ".campaign")
	require.NoError(t, os.MkdirAll(campaignDir, 0755))

	// Create a valid location to test with
	testLocation := filepath.Join(tmpDir, "projects")
	require.NoError(t, os.MkdirAll(testLocation, 0755))

	originalState := &State{
		LastLocation: testLocation,
	}

	// Save state
	err := SaveState(context.Background(), tmpDir, originalState)
	require.NoError(t, err, "SaveState should succeed")

	// Verify file exists
	stateFile := StatePath(tmpDir)
	_, err = os.Stat(stateFile)
	require.NoError(t, err, "state file should exist after SaveState")

	// Load and verify
	loadedState, err := LoadState(context.Background(), tmpDir)
	require.NoError(t, err, "LoadState should succeed")
	assert.Equal(t, testLocation, loadedState.LastLocation, "loaded location should match saved location")
	assert.False(t, loadedState.LastNavigation.IsZero(), "last navigation should be set")
}

func TestSetLastLocation_ValidDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	campaignDir := filepath.Join(tmpDir, ".campaign")
	require.NoError(t, os.MkdirAll(campaignDir, 0755))

	testLocation := filepath.Join(tmpDir, "projects")
	require.NoError(t, os.MkdirAll(testLocation, 0755))

	err := SetLastLocation(context.Background(), tmpDir, testLocation)
	require.NoError(t, err, "SetLastLocation should succeed for valid directory")

	// Verify it was saved
	saved, err := GetLastLocation(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.Equal(t, testLocation, saved)
}

func TestSetLastLocation_InvalidPath(t *testing.T) {
	tmpDir := t.TempDir()
	campaignDir := filepath.Join(tmpDir, ".campaign")
	require.NoError(t, os.MkdirAll(campaignDir, 0755))

	invalidPath := filepath.Join(tmpDir, "does-not-exist")

	err := SetLastLocation(context.Background(), tmpDir, invalidPath)
	require.Error(t, err, "SetLastLocation should error for non-existent path")
	assert.Contains(t, err.Error(), "does not exist")
}

func TestGetLastLocation_NoState(t *testing.T) {
	tmpDir := t.TempDir()

	location, err := GetLastLocation(context.Background(), tmpDir)
	require.NoError(t, err, "GetLastLocation should not error when no state")
	assert.Equal(t, "", location, "should return empty string when no state")
}

func TestGetLastLocation_WithState(t *testing.T) {
	tmpDir := t.TempDir()
	campaignDir := filepath.Join(tmpDir, ".campaign")
	require.NoError(t, os.MkdirAll(campaignDir, 0755))

	testLocation := filepath.Join(tmpDir, "festivals")
	require.NoError(t, os.MkdirAll(testLocation, 0755))

	err := SetLastLocation(context.Background(), tmpDir, testLocation)
	require.NoError(t, err)

	location, err := GetLastLocation(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.Equal(t, testLocation, location)
}

func TestClearState_WithFile(t *testing.T) {
	tmpDir := t.TempDir()
	campaignDir := filepath.Join(tmpDir, ".campaign")
	require.NoError(t, os.MkdirAll(campaignDir, 0755))

	testLocation := filepath.Join(tmpDir, "projects")
	require.NoError(t, os.MkdirAll(testLocation, 0755))

	// Create state
	err := SetLastLocation(context.Background(), tmpDir, testLocation)
	require.NoError(t, err)

	// Verify it exists
	stateFile := StatePath(tmpDir)
	_, err = os.Stat(stateFile)
	require.NoError(t, err)

	// Clear it
	err = ClearState(context.Background(), tmpDir)
	require.NoError(t, err)

	// Verify it's gone
	_, err = os.Stat(stateFile)
	require.True(t, os.IsNotExist(err), "state file should be deleted")
}

func TestClearState_NoFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Should not error even if file doesn't exist (idempotent)
	err := ClearState(context.Background(), tmpDir)
	require.NoError(t, err, "ClearState should be idempotent")
}

func TestContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// All operations should respect context cancellation
	_, err := LoadState(ctx, tmpDir)
	require.Error(t, err, "LoadState should error with cancelled context")

	state := &State{LastLocation: "/tmp"}
	err = SaveState(ctx, tmpDir, state)
	require.Error(t, err, "SaveState should error with cancelled context")

	_, err = GetLastLocation(ctx, tmpDir)
	require.Error(t, err, "GetLastLocation should error with cancelled context")

	err = SetLastLocation(ctx, tmpDir, "/tmp")
	require.Error(t, err, "SetLastLocation should error with cancelled context")

	err = ClearState(ctx, tmpDir)
	require.Error(t, err, "ClearState should error with cancelled context")
}

func TestStatePath(t *testing.T) {
	root := "/campaign"
	expected := "/campaign/.campaign/state.yaml"

	result := StatePath(root)
	assert.Equal(t, expected, result)
}

func TestMultipleUpdates(t *testing.T) {
	tmpDir := t.TempDir()
	campaignDir := filepath.Join(tmpDir, ".campaign")
	require.NoError(t, os.MkdirAll(campaignDir, 0755))

	// Create multiple locations
	loc1 := filepath.Join(tmpDir, "projects")
	loc2 := filepath.Join(tmpDir, "festivals")
	require.NoError(t, os.MkdirAll(loc1, 0755))
	require.NoError(t, os.MkdirAll(loc2, 0755))

	// Save first location
	err := SetLastLocation(context.Background(), tmpDir, loc1)
	require.NoError(t, err)

	saved, err := GetLastLocation(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.Equal(t, loc1, saved)

	// Update to second location
	err = SetLastLocation(context.Background(), tmpDir, loc2)
	require.NoError(t, err)

	saved, err = GetLastLocation(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.Equal(t, loc2, saved, "should have updated to new location")
}
