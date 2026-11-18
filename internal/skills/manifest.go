package skills

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"gopkg.in/yaml.v3"
)

const (
	// ManifestFileName is the shipped-skills manifest inside .campaign/skills/.
	ManifestFileName = ".shipped.yaml"
	// ManifestSchemaVersion identifies the manifest format.
	ManifestSchemaVersion = "skills-shipped/v1alpha1"
	// BackupDirName holds copies of files replaced by a forced update.
	BackupDirName = ".backup"
)

// Manifest records the sha256 of each shipped skill file as camp last wrote it,
// so an unchanged older file can be told apart from a local edit.
type Manifest struct {
	SchemaVersion string                       `yaml:"schema_version"`
	CampVersion   string                       `yaml:"camp_version"`
	Bundles       map[string]map[string]string `yaml:"bundles"`
	// Providers names the plugin that wrote each plugin bundle, or records that
	// this camp declined it. A bundle without an entry came from camp.
	Providers ProviderRecords `yaml:"providers,omitempty"`
}

// ProviderRecords maps a plugin bundle to its record. It is read leniently,
// entry by entry: an entry that does not decode is skipped and every other
// entry kept, and a value that is not a mapping counts as no records. A hand
// edit never breaks a command that only refreshes camp's own bundles, and one
// bad entry never erases a decline beside it.
type ProviderRecords map[string]ProviderRecord

// UnmarshalYAML implements yaml.Unmarshaler.
func (p *ProviderRecords) UnmarshalYAML(value *yaml.Node) error {
	*p = nil
	if value.Kind != yaml.MappingNode {
		return nil
	}
	records := make(ProviderRecords)
	for i := 0; i+1 < len(value.Content); i += 2 {
		var slug string
		var rec ProviderRecord
		if value.Content[i].Decode(&slug) != nil || value.Content[i+1].Decode(&rec) != nil {
			continue
		}
		records[slug] = rec
	}
	if len(records) > 0 {
		*p = records
	}
	return nil
}

// ProviderRecord is the plugin that offered a bundle and the plugin version
// that last wrote it. A declined bundle keeps its record and has no files.
type ProviderRecord struct {
	Plugin   string `yaml:"plugin"`
	Version  string `yaml:"version,omitempty"`
	Declined bool   `yaml:"declined,omitempty"`
}

func (m *Manifest) recorded(bundle, rel string) (string, bool) {
	if m == nil {
		return "", false
	}
	hash, ok := m.Bundles[bundle][rel]
	return hash, ok
}

func (m *Manifest) sameRecords(other *Manifest) bool {
	sameBundles := maps.EqualFunc(m.records(), other.records(), func(a, b map[string]string) bool {
		return maps.Equal(a, b)
	})
	return sameBundles && maps.Equal(m.providers(), other.providers())
}

func (m *Manifest) records() map[string]map[string]string {
	if m == nil {
		return nil
	}
	return m.Bundles
}

func (m *Manifest) providers() ProviderRecords {
	if m == nil {
		return nil
	}
	return m.Providers
}

func (m *Manifest) setProvider(bundle string, rec ProviderRecord) {
	if m.Providers == nil {
		m.Providers = make(ProviderRecords)
	}
	m.Providers[bundle] = rec
}

func (m *Manifest) set(bundle, rel, hash string) {
	if m.Bundles == nil {
		m.Bundles = make(map[string]map[string]string)
	}
	if m.Bundles[bundle] == nil {
		m.Bundles[bundle] = make(map[string]string)
	}
	m.Bundles[bundle][rel] = hash
}

// ReadManifest reads the manifest from skillsFS, rooted at .campaign/skills/.
// A camp without a manifest yields nil and no error.
func ReadManifest(skillsFS fs.FS) (*Manifest, error) {
	data, err := fs.ReadFile(skillsFS, ManifestFileName)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, camperrors.Wrap(err, "read shipped skills manifest")
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, camperrors.Wrapf(err, "parse %s%s; remove it to rebuild the record", shippedSkillsPrefix, ManifestFileName)
	}
	return &m, nil
}

func writeManifest(skillsDir string, m *Manifest) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return camperrors.Wrap(err, "encode shipped skills manifest")
	}
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return camperrors.Wrap(err, "create skills directory")
	}
	if err := fsutil.WriteFileAtomically(filepath.Join(skillsDir, ManifestFileName), data, 0o644); err != nil {
		return camperrors.Wrap(err, "write shipped skills manifest")
	}
	return nil
}
