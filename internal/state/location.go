package state

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

const (
	// cacheDir is the directory under campaign root where cache files are stored.
	cacheDir = ".campaign/cache"
	// stateFile is the name of the state file.
	stateFile = "state.jsonl"
	// maxHistoryEntries is the maximum number of navigation entries to keep.
	maxHistoryEntries = 5
)

// NavigationEntry represents a single navigation history entry.
type NavigationEntry struct {
	Location string    `json:"location"`
	Time     time.Time `json:"ts"`
}

// StatePath returns the path to the state file for a given campaign root.
func StatePath(campaignRoot string) string {
	return filepath.Join(campaignRoot, cacheDir, stateFile)
}

// LoadHistory loads all navigation entries from the state file.
// Returns empty slice if the file doesn't exist (no error).
// Corrupt lines are skipped with a warning so one torn entry does not brick navigation.
// Returns error only for actual I/O problems.
func LoadHistory(ctx context.Context, campaignRoot string) ([]NavigationEntry, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	stateFile := StatePath(campaignRoot)
	file, err := os.Open(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []NavigationEntry{}, nil
		}
		return nil, fmt.Errorf("failed to open state file %s: %w", stateFile, err)
	}
	defer file.Close()

	var entries []NavigationEntry
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}
		var entry NavigationEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			fmt.Fprintf(os.Stderr, "camp: state: skipping corrupt line %d in %s: %v\n", lineNum, stateFile, err)
			continue
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	return entries, nil
}

// SaveEntry appends a navigation entry to the state file.
// If the file exceeds maxHistoryEntries, it truncates to the last maxHistoryEntries.
func SaveEntry(ctx context.Context, campaignRoot string, entry NavigationEntry) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	stateFilePath := StatePath(campaignRoot)
	stateDir := filepath.Dir(stateFilePath)

	// Ensure cache directory exists
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Load existing entries
	entries, err := LoadHistory(ctx, campaignRoot)
	if err != nil {
		// If we can't load, start fresh
		entries = []NavigationEntry{}
	}

	// Append new entry
	entries = append(entries, entry)

	// Truncate to last maxHistoryEntries
	if len(entries) > maxHistoryEntries {
		entries = entries[len(entries)-maxHistoryEntries:]
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("failed to marshal entry: %w", err)
		}
	}

	if err := fsutil.WriteFileAtomically(stateFilePath, buf.Bytes(), 0600); err != nil {
		return camperrors.Wrap(err, "failed to write state file")
	}
	return nil
}

// GetLastN returns the last N navigation entries, most recent last.
// Returns fewer entries if history has less than N entries.
func GetLastN(ctx context.Context, campaignRoot string, n int) ([]NavigationEntry, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	entries, err := LoadHistory(ctx, campaignRoot)
	if err != nil {
		return nil, err
	}

	if len(entries) <= n {
		return entries, nil
	}

	return entries[len(entries)-n:], nil
}

// GetLastLocation retrieves the most recent location from navigation history.
// Returns empty string if no location has been saved yet.
// Returns error only for I/O or parsing problems, not missing state.
func GetLastLocation(ctx context.Context, campaignRoot string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	entries, err := LoadHistory(ctx, campaignRoot)
	if err != nil {
		return "", err
	}

	if len(entries) == 0 {
		return "", nil
	}

	return entries[len(entries)-1].Location, nil
}

// SetLastLocation saves a new location to the navigation history.
// Validates that the location exists and is a directory.
func SetLastLocation(ctx context.Context, campaignRoot, location string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Validate that the location exists
	if info, err := os.Stat(location); err != nil || !info.IsDir() {
		return fmt.Errorf("invalid location: %s does not exist or is not a directory", location)
	}

	entry := NavigationEntry{
		Location: location,
		Time:     time.Now(),
	}

	return SaveEntry(ctx, campaignRoot, entry)
}

// ClearState removes the state file, resetting navigation history.
// Returns nil if the file doesn't exist (idempotent).
func ClearState(ctx context.Context, campaignRoot string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	stateFilePath := StatePath(campaignRoot)
	if err := os.Remove(stateFilePath); err != nil {
		if os.IsNotExist(err) {
			return nil // Idempotent - no state to clear
		}
		return fmt.Errorf("failed to remove state file: %w", err)
	}

	return nil
}
