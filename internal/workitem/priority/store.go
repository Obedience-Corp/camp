package priority

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/workitem"
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
		return nil, camperrors.Wrapf(err, "reading priority store %s", path)
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, camperrors.Wrapf(err, "parsing priority store %s", path)
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
		return camperrors.Wrapf(err, "creating priority store directory %s", dir)
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "marshaling priority store")
	}
	data = append(data, '\n')

	if err := fsutil.WriteFileAtomically(path, data, 0o644); err != nil {
		return camperrors.Wrap(err, "writing priority store")
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

// Apply decorates each WorkItem with its stored manual priority. Items not in
// the store have their ManualPriority cleared to ensure idempotency after Clear.
func Apply(store *Store, items []workitem.WorkItem) []workitem.WorkItem {
	for i := range items {
		if entry, ok := store.ManualPriorities[items[i].Key]; ok {
			items[i].ManualPriority = string(entry.Priority)
		} else {
			items[i].ManualPriority = ""
		}
	}
	return items
}

// SaveOrDelete saves the store if it contains entries, or deletes the file if empty.
func SaveOrDelete(path string, store *Store) error {
	if IsEmpty(store) {
		err := os.Remove(path)
		if err != nil && !os.IsNotExist(err) {
			return camperrors.Wrapf(err, "removing empty priority store %s", path)
		}
		return nil
	}
	return Save(path, store)
}

// IsEmpty reports whether the store has no priority entries.
func IsEmpty(store *Store) bool {
	return len(store.ManualPriorities) == 0
}
