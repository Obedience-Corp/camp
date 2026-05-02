package pins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// Pin represents a saved pinned directory.
//
// Path is set for in-tree pins and stored relative to the campaign root so
// the pins file is portable when the campaign moves.
//
// AbsPath is set for attachment pins — pins targeting a directory outside
// the campaign tree that is bound to this campaign via a Kind="attachment"
// marker. Exactly one of Path or AbsPath should be set on a given pin.
type Pin struct {
	Name      string    `json:"name"`
	Path      string    `json:"path,omitempty"`
	AbsPath   string    `json:"abs_path,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Store manages pin persistence.
type Store struct {
	path string
	pins []Pin
}

// NewStore creates a store backed by the given JSON file path.
func NewStore(filePath string) *Store {
	return &Store{path: filePath}
}

// Load reads pins from disk. Returns empty list if file doesn't exist.
func (s *Store) Load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.pins = nil
			return nil
		}
		return camperrors.Wrap(err, "read pins")
	}
	if len(data) == 0 {
		s.pins = nil
		return nil
	}
	if err := json.Unmarshal(data, &s.pins); err != nil {
		return camperrors.Wrapf(err, "parse pins file %s", s.path)
	}
	return nil
}

// Save writes pins to disk with atomic write.
func (s *Store) Save() error {
	data, err := json.MarshalIndent(s.pins, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "marshal pins")
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return camperrors.Wrap(err, "create pins directory")
	}

	if err := fsutil.WriteFileAtomically(s.path, data, 0o644); err != nil {
		return camperrors.Wrap(err, "write pins file")
	}
	return nil
}

// List returns all pins.
func (s *Store) List() []Pin {
	return s.pins
}

// Get returns a pin by name, or false if not found.
func (s *Store) Get(name string) (Pin, bool) {
	for _, p := range s.pins {
		if p.Name == name {
			return p, true
		}
	}
	return Pin{}, false
}

// Add creates a new pin. Returns error if name already exists.
func (s *Store) Add(name, path string) error {
	if _, exists := s.Get(name); exists {
		return camperrors.Wrapf(camperrors.ErrAlreadyExists,
			"pin %q already exists (use 'camp unpin %s' first)", name, name)
	}
	s.pins = append(s.pins, Pin{
		Name:      name,
		Path:      path,
		CreatedAt: time.Now(),
	})
	return nil
}

// Remove deletes a pin by name. Returns error if not found.
func (s *Store) Remove(name string) error {
	for i, p := range s.pins {
		if p.Name == name {
			s.pins = append(s.pins[:i], s.pins[i+1:]...)
			return nil
		}
	}
	return camperrors.Wrapf(camperrors.ErrNotFound, "pin %q not found", name)
}

// FindByPath returns the pin whose path matches, or false if not found.
func (s *Store) FindByPath(path string) (Pin, bool) {
	for _, p := range s.pins {
		if p.Path == path {
			return p, true
		}
	}
	return Pin{}, false
}

// ToggleResult describes what happened during a toggle operation.
type ToggleResult int

const (
	// Pinned indicates a new pin was created.
	Pinned ToggleResult = iota
	// Unpinned indicates an existing pin was removed (same path toggle).
	Unpinned
	// Updated indicates an existing pin's path was changed.
	Updated
)

// Toggle adds, removes, or updates a pin by name.
// If name does not exist: adds it (returns Pinned).
// If name exists with the same path: removes it (returns Unpinned).
// If name exists with a different path: updates the path (returns Updated).
func (s *Store) Toggle(name, path string) ToggleResult {
	for i, p := range s.pins {
		if p.Name == name {
			if p.Path == path {
				s.pins = append(s.pins[:i], s.pins[i+1:]...)
				return Unpinned
			}
			s.pins[i].Path = path
			return Updated
		}
	}
	s.pins = append(s.pins, Pin{
		Name:      name,
		Path:      path,
		CreatedAt: time.Now(),
	})
	return Pinned
}

// MigrateAbsoluteToRelative converts absolute paths to campaign-root-relative
// paths. Pins outside the campaign root are dropped. Returns true if any
// pins were converted or removed.
func (s *Store) MigrateAbsoluteToRelative(root string) bool {
	// Canonicalize root so comparison is consistent even if caller
	// passes a non-symlink-resolved path.
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}

	changed := false
	for i := len(s.pins) - 1; i >= 0; i-- {
		p := s.pins[i]
		if filepath.IsAbs(p.Path) {
			// Try symlink-resolved path first, fall back to cleaned original
			// (handles deleted directories whose symlink can't be resolved)
			resolved := p.Path
			if r, err := filepath.EvalSymlinks(p.Path); err == nil {
				resolved = r
			}

			rel, err := filepath.Rel(root, resolved)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
				s.pins[i].Path = rel
				changed = true
			} else if resolved != p.Path {
				// EvalSymlinks changed the path but Rel failed — try with
				// the original cleaned path in case the resolved form
				// diverged (e.g. /tmp vs /private/tmp on macOS for deleted dirs)
				rel2, err2 := filepath.Rel(root, filepath.Clean(p.Path))
				if err2 == nil && rel2 != ".." && !strings.HasPrefix(rel2, "../") {
					s.pins[i].Path = rel2
					changed = true
				} else {
					s.pins = append(s.pins[:i], s.pins[i+1:]...)
					changed = true
				}
			} else {
				// External pin — remove from list
				s.pins = append(s.pins[:i], s.pins[i+1:]...)
				changed = true
			}
		}
	}
	return changed
}

// MigrateLegacyStore moves a legacy pins.json into the current store location
// and rewrites any absolute paths to campaign-root-relative paths.
func MigrateLegacyStore(root, oldPath, newPath string) {
	if _, err := os.Stat(oldPath); err == nil {
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			_ = os.MkdirAll(filepath.Dir(newPath), 0755)
			_ = os.Rename(oldPath, newPath)
		}
	}

	store := NewStore(newPath)
	if err := store.Load(); err != nil {
		return
	}
	if store.MigrateAbsoluteToRelative(root) {
		_ = store.Save()
	}
}

// Names returns all pin names (for tab completion).
func (s *Store) Names() []string {
	names := make([]string, len(s.pins))
	for i, p := range s.pins {
		names[i] = p.Name
	}
	return names
}
