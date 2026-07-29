package machines

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"gopkg.in/yaml.v3"
)

// declinedFileName sits beside machines.yaml rather than inside it. A decline is
// a camp-terminal concept with no meaning to the festival app, which mirrors
// machines.yaml field-for-field, and a declined origin is by definition not a
// machine, so it has no row to attach to. A separate file is also strictly safer
// for version skew: an older camp does not read it at all, so it cannot misparse
// it, and the worst it does is prompt again.
const declinedFileName = "declined_origins.yaml"

// DeclinedFile is the on-disk shape of declined_origins.yaml.
type DeclinedFile struct {
	Version  int            `yaml:"version"`
	Declined []DeclinedHost `yaml:"declined"`
}

// DeclinedHost records one origin the operator said no to. Host is the identity
// that matters: the same machine arriving under a different suggested id is the
// same machine, and a decline a re-derived id could bypass would re-prompt
// exactly the operator who declined. Id is kept for display.
type DeclinedHost struct {
	ID         string    `yaml:"id,omitempty"`
	Host       string    `yaml:"host"`
	DeclinedAt time.Time `yaml:"declined_at"`
}

// DeclinedPath returns the decline file path, resolved from MachinesPath so the
// CAMP_MACHINES_PATH and XDG_CONFIG_HOME overrides isolate both files together.
func DeclinedPath() string {
	return filepath.Join(filepath.Dir(MachinesPath()), declinedFileName)
}

// LoadDeclined reads the decline list. An absent file is not an error: it is the
// normal state. A corrupt one is reported so the caller can warn, but callers
// treat it as empty, because a broken suppression list must never prevent an
// operator from adopting.
func LoadDeclined() (*DeclinedFile, error) {
	data, err := os.ReadFile(DeclinedPath())
	if errors.Is(err, fs.ErrNotExist) {
		return &DeclinedFile{Version: currentVersion}, nil
	}
	if err != nil {
		return &DeclinedFile{Version: currentVersion}, camperrors.Wrap(err, "read declined origins file")
	}
	var f DeclinedFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return &DeclinedFile{Version: currentVersion}, camperrors.Wrap(err, "parse declined origins file")
	}
	return &f, nil
}

// Save writes f atomically with 0600 perms, matching machines.yaml: the file
// names hosts an operator chose not to register.
func (f *DeclinedFile) Save() error {
	path := DeclinedPath()
	f.Version = currentVersion
	data, err := yaml.Marshal(f)
	if err != nil {
		return camperrors.Wrap(err, "encode declined origins")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return camperrors.Wrap(err, "create declined origins directory")
	}
	if err := fsutil.WriteFileAtomically(path, data, 0o600); err != nil {
		return camperrors.Wrap(err, "write declined origins file")
	}
	return nil
}

// IsDeclined reports whether host was declined, matching on the normalized host
// so case and a trailing FQDN dot do not create a second identity for one
// machine.
func (f *DeclinedFile) IsDeclined(host string) (DeclinedHost, bool) {
	want := NormalizeHost(host)
	for _, d := range f.Declined {
		if NormalizeHost(d.Host) == want {
			return d, true
		}
	}
	return DeclinedHost{}, false
}

// Decline records a decline, replacing any earlier one for the same host so the
// timestamp reflects the most recent answer.
func (f *DeclinedFile) Decline(id, host string, at time.Time) {
	f.Remove(host)
	f.Declined = append(f.Declined, DeclinedHost{ID: id, Host: host, DeclinedAt: at.UTC()})
}

// Remove drops any decline for host. Adopting clears the memory: the origin is
// no longer declined.
func (f *DeclinedFile) Remove(host string) {
	want := NormalizeHost(host)
	kept := f.Declined[:0]
	for _, d := range f.Declined {
		if NormalizeHost(d.Host) != want {
			kept = append(kept, d)
		}
	}
	f.Declined = kept
}

// NormalizeHost trims whitespace and the trailing dot MagicDNS names carry, then
// lowercases. Hostnames are case-insensitive, so "MAC-STUDIO.ts.net." and
// "mac-studio.ts.net" are one machine.
func NormalizeHost(h string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), "."))
}
