package pins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Pin represents a bookmarked directory.
type Pin struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
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
		return fmt.Errorf("read pins: %w", err)
	}
	if len(data) == 0 {
		s.pins = nil
		return nil
	}
	if err := json.Unmarshal(data, &s.pins); err != nil {
		return fmt.Errorf("parse pins file %s: %w", s.path, err)
	}
	return nil
}

// Save writes pins to disk with atomic write.
func (s *Store) Save() error {
	data, err := json.MarshalIndent(s.pins, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pins: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create pins directory: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write pins tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename pins file: %w", err)
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
		return fmt.Errorf("pin %q already exists (use 'camp unpin %s' first)", name, name)
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
	return fmt.Errorf("pin %q not found", name)
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

// MigrateAbsoluteToRelative converts any absolute paths to paths relative to
// the given root. Returns true if any paths were converted.
func (s *Store) MigrateAbsoluteToRelative(root string) bool {
	changed := false
	for i := len(s.pins) - 1; i >= 0; i-- {
		p := s.pins[i]
		if filepath.IsAbs(p.Path) {
			rel, err := filepath.Rel(root, p.Path)
			if err == nil && !strings.HasPrefix(rel, "..") {
				s.pins[i].Path = rel
				changed = true
			} else {
				// External pin — remove from list
				s.pins = append(s.pins[:i], s.pins[i+1:]...)
				changed = true
			}
		}
	}
	return changed
}

// Names returns all pin names (for tab completion).
func (s *Store) Names() []string {
	names := make([]string, len(s.pins))
	for i, p := range s.pins {
		names[i] = p.Name
	}
	return names
}
