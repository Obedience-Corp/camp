package machines

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"gopkg.in/yaml.v3"
)

// Load reads ~/.obey/machines.yaml into a typed File. A missing file is not an
// error: it yields an empty, version-stamped set. That absent-file => zero-
// machines path is the guarantee every downstream camp list/switch relies on to
// stay byte-identical when no fleet is configured. Unknown/future YAML fields
// are ignored, not fatal, so a file written by a newer version still loads.
func Load() (*File, error) {
	path := MachinesPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &File{Version: currentVersion}, nil
	}
	if err != nil {
		return nil, camperrors.Wrap(err, "read machines file")
	}
	return decode(data)
}

// decode parses machines.yaml bytes into the typed File. KnownFields stays off,
// so unknown keys are tolerated (version-skew). Pure; no filesystem access.
func decode(data []byte) (*File, error) {
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, camperrors.Wrap(err, "parse machines file")
	}
	return &f, nil
}

// Save writes f atomically to ~/.obey/machines.yaml with 0600 perms (the file
// names ssh identities and hosts). The reserved id "local" is never persisted,
// even if present in f.Machines, keeping the current machine implicit.
func (f *File) Save() error {
	path := MachinesPath()
	data, err := encode(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return camperrors.Wrap(err, "create machines file directory")
	}
	if err := fsutil.WriteFileAtomically(path, data, 0o600); err != nil {
		return camperrors.Wrap(err, "write machines file")
	}
	return nil
}

// encode produces the on-disk bytes for f: it stamps the current schema version
// and drops any machine whose id is the reserved LocalMachineID. Pure; no
// filesystem access, so the local-never-persisted guarantee is host-testable.
func encode(f *File) ([]byte, error) {
	out := File{Version: currentVersion}
	for _, m := range f.Machines {
		if m.ID == LocalMachineID {
			continue
		}
		out.Machines = append(out.Machines, m)
	}
	data, err := yaml.Marshal(&out)
	if err != nil {
		return nil, camperrors.Wrap(err, "marshal machines file")
	}
	return data, nil
}

// Lookup resolves a machine id against the file. The reserved LocalMachineID
// (and "") mean the current machine and return (nil, true, true) so callers take
// the local path. An unknown id returns found=false — distinct from the
// zero-machines case, so callers can error "unknown machine <id>".
func (f *File) Lookup(id string) (m *Machine, isLocal bool, found bool) {
	if id == "" || id == LocalMachineID {
		return nil, true, true
	}
	for i := range f.Machines {
		if f.Machines[i].ID == id {
			return &f.Machines[i], false, true
		}
	}
	return nil, false, false
}
