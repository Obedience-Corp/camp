package priority

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StorePath returns the absolute path to the priority store file within a campaign.
func StorePath(campaignRoot string) string {
	return filepath.Join(campaignRoot, ".campaign", "settings", "workitems.json")
}

// Load reads the priority store from disk. Returns an empty store if the file
// does not exist. Returns a wrapped error for invalid JSON or read failures.
func Load(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return NewStore(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading priority store %s: %w", path, err)
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing priority store %s: %w", path, err)
	}
	if s.ManualPriorities == nil {
		s.ManualPriorities = make(map[string]PriorityEntry)
	}
	return &s, nil
}

// Save atomically writes the store to disk. It writes to a temp file in the same
// directory then renames into place, creating parent directories as needed.
func Save(path string, store *Store) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating priority store directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling priority store: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, "workitems-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file for priority store: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing priority store temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing priority store temp file: %w", err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("renaming priority store temp file: %w", err)
	}
	return nil
}

// Set adds or updates a priority entry for the given key.
func Set(store *Store, key string, p ManualPriority) {
	store.ManualPriorities[key] = PriorityEntry{
		Priority:  p,
		UpdatedAt: time.Now().UTC(),
	}
}

// Clear removes the priority entry for the given key.
func Clear(store *Store, key string) {
	delete(store.ManualPriorities, key)
}

// Prune removes entries whose keys are not in validKeys. Returns true if any
// entries were removed. validKeys must be the full unfiltered set of discovered
// work item keys — never a filtered subset.
func Prune(store *Store, validKeys map[string]bool) bool {
	var stale []string
	for k := range store.ManualPriorities {
		if !validKeys[k] {
			stale = append(stale, k)
		}
	}
	for _, k := range stale {
		delete(store.ManualPriorities, k)
	}
	return len(stale) > 0
}

// TODO: Apply(store *Store, items []workitem.WorkItem) []workitem.WorkItem
// Deferred until WorkItem.ManualPriority field is added in sequence 02.

// SaveOrDelete saves the store if it contains entries, or deletes the file if empty.
func SaveOrDelete(path string, store *Store) error {
	if IsEmpty(store) {
		err := os.Remove(path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing empty priority store %s: %w", path, err)
		}
		return nil
	}
	return Save(path, store)
}

// IsEmpty reports whether the store has no priority entries.
func IsEmpty(store *Store) bool {
	return len(store.ManualPriorities) == 0
}
