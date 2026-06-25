package priority

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

var groupShape = regexp.MustCompile(`^[a-z0-9_][a-z0-9_-]{0,79}$`)

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
	if s.Attention == nil {
		s.Attention = make(map[string]AttentionEntry)
	}
	if s.Version == 0 {
		s.Version = 1
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

func SetAttentionStage(store *Store, key string, stage AttentionStage) {
	entry := store.Attention[key]
	entry.Stage = stage
	entry.UpdatedAt = time.Now().UTC()
	store.Attention[key] = entry
	store.Version = 2
}

func ClearAttentionStage(store *Store, key string) {
	entry, ok := store.Attention[key]
	if !ok {
		return
	}
	entry.Stage = AttentionNone
	entry.UpdatedAt = time.Now().UTC()
	if entry.Group == "" {
		delete(store.Attention, key)
		return
	}
	store.Attention[key] = entry
	store.Version = 2
}

func SetGroup(store *Store, key, group string) {
	entry := store.Attention[key]
	entry.Group = group
	entry.UpdatedAt = time.Now().UTC()
	store.Attention[key] = entry
	store.Version = 2
}

func ClearGroup(store *Store, key string) {
	entry, ok := store.Attention[key]
	if !ok {
		return
	}
	entry.Group = ""
	entry.UpdatedAt = time.Now().UTC()
	if entry.Stage == AttentionNone {
		delete(store.Attention, key)
		return
	}
	store.Attention[key] = entry
	store.Version = 2
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
	var staleAttention []string
	for k := range store.Attention {
		if !validKeys[k] {
			staleAttention = append(staleAttention, k)
		}
	}
	for _, k := range staleAttention {
		delete(store.Attention, k)
	}
	return len(stale) > 0 || len(staleAttention) > 0
}

// ValidKeys returns the full set of item keys that may retain priority entries.
func ValidKeys(items []workitem.WorkItem) map[string]bool {
	validKeys := make(map[string]bool, len(items))
	for _, item := range items {
		validKeys[item.Key] = true
	}
	return validKeys
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
		ApplyAttentionToItem(store, &items[i])
	}
	return items
}

func ApplyAttentionToItem(store *Store, item *workitem.WorkItem) {
	item.AttentionStage = ""
	item.AttentionStageSource = "none"
	item.Group = ""
	if EligibleForAttention(*item) {
		item.AttentionStage = string(AttentionActive)
		item.AttentionStageSource = "derived"
	} else {
		return
	}
	if store == nil {
		return
	}
	entry, ok := store.Attention[item.Key]
	if !ok {
		return
	}
	if entry.Stage != AttentionNone {
		item.AttentionStage = string(entry.Stage)
		item.AttentionStageSource = "explicit"
	}
	item.Group = entry.Group
}

func EligibleForAttention(item workitem.WorkItem) bool {
	return item.ItemKind == workitem.ItemKindDirectory &&
		(item.WorkflowType == workitem.WorkflowTypeDesign ||
			item.WorkflowType == workitem.WorkflowTypeExplore ||
			item.LifecycleStage == workitem.LifecycleStageNone)
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

// WithLock holds an exclusive lock for a full load-mutate-save cycle.
// The store is re-loaded inside the lock so concurrent priority mutations do
// not overwrite each other. On success, the store is saved or deleted using the
// same SaveOrDelete contract as direct callers.
func WithLock(ctx context.Context, storePath string, fn func(*Store) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		return camperrors.Wrap(err, "create priority store directory")
	}

	release, err := fsutil.AcquireFileLock(ctx, storePath+".lock")
	if err != nil {
		return err
	}
	defer release()

	store, err := Load(storePath)
	if err != nil {
		return err
	}
	if err := fn(store); err != nil {
		return err
	}
	return SaveOrDelete(storePath, store)
}

// IsEmpty reports whether the store has no priority entries.
func IsEmpty(store *Store) bool {
	return len(store.ManualPriorities) == 0 && len(store.Attention) == 0
}

func ValidGroup(group string) bool {
	return groupShape.MatchString(group)
}
