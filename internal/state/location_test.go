package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadHistory_NoFile(t *testing.T) {
	tmpDir := t.TempDir()

	entries, err := LoadHistory(context.Background(), tmpDir)
	require.NoError(t, err, "LoadHistory should not error when state file doesn't exist")
	assert.NotNil(t, entries, "should return empty slice, not nil")
	assert.Len(t, entries, 0, "empty history should have no entries")
}

func TestSaveEntryAndLoadHistory(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, ".campaign", "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Create a valid location to test with
	testLocation := filepath.Join(tmpDir, "projects")
	require.NoError(t, os.MkdirAll(testLocation, 0755))

	// Save entry using SetLastLocation
	err := SetLastLocation(context.Background(), tmpDir, testLocation)
	require.NoError(t, err, "SetLastLocation should succeed")

	// Verify file exists
	stateFile := StatePath(tmpDir)
	_, err = os.Stat(stateFile)
	require.NoError(t, err, "state file should exist after SetLastLocation")

	// Load and verify
	entries, err := LoadHistory(context.Background(), tmpDir)
	require.NoError(t, err, "LoadHistory should succeed")
	require.Len(t, entries, 1, "should have one entry")
	assert.Equal(t, testLocation, entries[0].Location, "loaded location should match saved location")
	assert.False(t, entries[0].Time.IsZero(), "timestamp should be set")
}

func TestSetLastLocation_ValidDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, ".campaign", "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

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
	cacheDir := filepath.Join(tmpDir, ".campaign", "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

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
	cacheDir := filepath.Join(tmpDir, ".campaign", "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

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
	cacheDir := filepath.Join(tmpDir, ".campaign", "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

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
	_, err := LoadHistory(ctx, tmpDir)
	require.Error(t, err, "LoadHistory should error with cancelled context")

	_, err = GetLastLocation(ctx, tmpDir)
	require.Error(t, err, "GetLastLocation should error with cancelled context")

	err = SetLastLocation(ctx, tmpDir, "/tmp")
	require.Error(t, err, "SetLastLocation should error with cancelled context")

	err = ClearState(ctx, tmpDir)
	require.Error(t, err, "ClearState should error with cancelled context")
}

func TestStatePath(t *testing.T) {
	root := "/campaign"
	expected := "/campaign/.campaign/cache/state.jsonl"

	result := StatePath(root)
	assert.Equal(t, expected, result)
}

func TestMultipleUpdates(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, ".campaign", "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

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

	// Verify both entries exist in history
	entries, err := LoadHistory(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "should have two entries in history")
	assert.Equal(t, loc1, entries[0].Location)
	assert.Equal(t, loc2, entries[1].Location)
}

func TestBoundedHistory(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, ".campaign", "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Create 7 locations
	locations := make([]string, 7)
	for i := range locations {
		locations[i] = filepath.Join(tmpDir, "loc", string(rune('a'+i)))
		require.NoError(t, os.MkdirAll(locations[i], 0755))
	}

	// Save all 7 locations
	for _, loc := range locations {
		err := SetLastLocation(context.Background(), tmpDir, loc)
		require.NoError(t, err)
	}

	// Should only have last 5 entries
	entries, err := LoadHistory(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.Len(t, entries, 5, "should have exactly 5 entries after adding 7")

	// Verify we kept the last 5 (indices 2-6 of original)
	for i, entry := range entries {
		expectedLoc := locations[i+2] // Offset by 2 since first 2 were truncated
		assert.Equal(t, expectedLoc, entry.Location, "entry %d should be location %d", i, i+2)
	}
}

func TestGetLastN(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, ".campaign", "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Create 5 locations
	locations := make([]string, 5)
	for i := range locations {
		locations[i] = filepath.Join(tmpDir, "loc", string(rune('a'+i)))
		require.NoError(t, os.MkdirAll(locations[i], 0755))
		err := SetLastLocation(context.Background(), tmpDir, locations[i])
		require.NoError(t, err)
	}

	// Get last 3
	entries, err := GetLastN(context.Background(), tmpDir, 3)
	require.NoError(t, err)
	assert.Len(t, entries, 3, "should have 3 entries")
	assert.Equal(t, locations[2], entries[0].Location)
	assert.Equal(t, locations[3], entries[1].Location)
	assert.Equal(t, locations[4], entries[2].Location)

	// Get more than available
	entries, err = GetLastN(context.Background(), tmpDir, 10)
	require.NoError(t, err)
	assert.Len(t, entries, 5, "should return all 5 entries when requesting 10")
}

func TestGetLastN_NoHistory(t *testing.T) {
	tmpDir := t.TempDir()

	entries, err := GetLastN(context.Background(), tmpDir, 3)
	require.NoError(t, err)
	assert.Len(t, entries, 0, "should return empty slice when no history")
}

func TestJSONLFormat(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, ".campaign", "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	testLocation := filepath.Join(tmpDir, "projects")
	require.NoError(t, os.MkdirAll(testLocation, 0755))

	err := SetLastLocation(context.Background(), tmpDir, testLocation)
	require.NoError(t, err)

	// Read the raw file and verify it's valid JSONL
	data, err := os.ReadFile(StatePath(tmpDir))
	require.NoError(t, err)

	// Should be a single line with newline at end
	lines := string(data)
	assert.Contains(t, lines, `"location":`)
	assert.Contains(t, lines, `"ts":`)
	assert.True(t, lines[len(lines)-1] == '\n', "file should end with newline")
}
