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
}

func (m *Manifest) recorded(bundle, rel string) (string, bool) {
	if m == nil {
		return "", false
	}
	hash, ok := m.Bundles[bundle][rel]
	return hash, ok
}

func (m *Manifest) sameRecords(other *Manifest) bool {
	return maps.EqualFunc(m.records(), other.records(), func(a, b map[string]string) bool {
		return maps.Equal(a, b)
	})
}

func (m *Manifest) records() map[string]map[string]string {
	if m == nil {
		return nil
	}
	return m.Bundles
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
