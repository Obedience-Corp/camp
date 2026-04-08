package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// LinkedProjectManifest stores metadata about projects linked into the campaign
// via symlinks rather than git submodules. This file is machine-local and
// should be gitignored.
type LinkedProjectManifest struct {
	Projects map[string]LinkedProjectEntry `json:"projects"`
}

// LinkedProjectEntry records how a linked project was added.
type LinkedProjectEntry struct {
	// Source is the project source type (SourceLinked or SourceLinkedNonGit).
	Source string `json:"source"`
	// AbsPath is the absolute path to the original project on disk.
	AbsPath string `json:"abs_path"`
	// IsGit indicates whether the linked project is a git repository.
	IsGit bool `json:"is_git"`
	// AddedAt is the timestamp when the project was linked.
	AddedAt time.Time `json:"added_at"`
}

const linkedProjectsFile = "linked-projects.json"

// manifestPath returns the path to the linked projects manifest.
func manifestPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, ".campaign", linkedProjectsFile)
}

var manifestMu sync.Mutex

// LoadManifest reads the linked projects manifest from disk.
// Returns an empty manifest if the file doesn't exist.
func LoadManifest(campaignRoot string) (*LinkedProjectManifest, error) {
	path := manifestPath(campaignRoot)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &LinkedProjectManifest{
				Projects: make(map[string]LinkedProjectEntry),
			}, nil
		}
		return nil, camperrors.Wrapf(err, "read linked projects manifest")
	}

	var m LinkedProjectManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, camperrors.Wrapf(err, "parse linked projects manifest")
	}

	if m.Projects == nil {
		m.Projects = make(map[string]LinkedProjectEntry)
	}

	return &m, nil
}

// SaveManifest writes the linked projects manifest to disk atomically.
func SaveManifest(campaignRoot string, m *LinkedProjectManifest) error {
	manifestMu.Lock()
	defer manifestMu.Unlock()

	path := manifestPath(campaignRoot)

	// Ensure .campaign directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return camperrors.Wrapf(err, "create .campaign directory")
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return camperrors.Wrapf(err, "marshal linked projects manifest")
	}
	data = append(data, '\n')

	// Write atomically via temp file
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return camperrors.Wrapf(err, "write linked projects manifest")
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return camperrors.Wrapf(err, "rename linked projects manifest")
	}

	return nil
}

// AddToManifest adds a linked project entry and saves the manifest.
func AddToManifest(campaignRoot, name string, entry LinkedProjectEntry) error {
	m, err := LoadManifest(campaignRoot)
	if err != nil {
		return err
	}

	m.Projects[name] = entry

	return SaveManifest(campaignRoot, m)
}

// RemoveFromManifest removes a linked project entry and saves the manifest.
// Returns true if the entry existed.
func RemoveFromManifest(campaignRoot, name string) (bool, error) {
	m, err := LoadManifest(campaignRoot)
	if err != nil {
		return false, err
	}

	if _, exists := m.Projects[name]; !exists {
		return false, nil
	}

	delete(m.Projects, name)

	return true, SaveManifest(campaignRoot, m)
}

// IsLinkedProject checks whether a project name is in the linked manifest.
func IsLinkedProject(campaignRoot, name string) (bool, *LinkedProjectEntry, error) {
	m, err := LoadManifest(campaignRoot)
	if err != nil {
		return false, nil, err
	}

	entry, exists := m.Projects[name]
	if !exists {
		return false, nil, nil
	}

	return true, &entry, nil
}
