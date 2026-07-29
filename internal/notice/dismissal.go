package notice

import (
	"os"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"gopkg.in/yaml.v3"
)

// DismissalRelPath is where dismissals live, campaign-relative.
//
// Committed, not cached. A dismissal is a judgement the user made about their
// own campaign ("yes, I know, stop telling me"), and that judgement should
// travel to their other machines the same way the artifact declarations it
// concerns do. Putting it under .campaign/cache would make the same notice
// reappear on every machine, which is how an advisory becomes noise.
const DismissalRelPath = ".campaign/notices.yaml"

// DismissalFile is the on-disk shape of .campaign/notices.yaml.
type DismissalFile struct {
	Version int `yaml:"version"`
	// Dismissed maps a notice ID to when it was dismissed. Keyed by ID rather
	// than by notice kind, so a signal that recurs for a new subject (a newly
	// declared root, say) produces a new ID and notifies again.
	Dismissed map[string]time.Time `yaml:"dismissed,omitempty"`
}

// DismissalPath returns the absolute path of the dismissal file.
func DismissalPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, filepath.FromSlash(DismissalRelPath))
}

// LoadDismissals reads the dismissal file. A missing file is an empty set, not
// an error: no dismissals is the normal starting state.
func LoadDismissals(campaignRoot string) (*DismissalFile, error) {
	data, err := os.ReadFile(DismissalPath(campaignRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return &DismissalFile{Version: 1, Dismissed: map[string]time.Time{}}, nil
		}
		return nil, camperrors.Wrap(err, "read notice dismissals")
	}

	var f DismissalFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, camperrors.Wrap(err, "parse notice dismissals")
	}
	if f.Version == 0 {
		f.Version = 1
	}
	if f.Dismissed == nil {
		f.Dismissed = map[string]time.Time{}
	}
	return &f, nil
}

// Save writes the dismissal file atomically.
func (f *DismissalFile) Save(campaignRoot string) error {
	path := DismissalPath(campaignRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return camperrors.Wrap(err, "create .campaign directory")
	}
	data, err := yaml.Marshal(f)
	if err != nil {
		return camperrors.Wrap(err, "encode notice dismissals")
	}
	if err := fsutil.WriteFileAtomically(path, data, 0o644); err != nil {
		return camperrors.Wrap(err, "write notice dismissals")
	}
	return nil
}

// IsDismissed reports whether a notice ID has been dismissed.
func (f *DismissalFile) IsDismissed(id string) bool {
	if f == nil || id == "" {
		return false
	}
	_, ok := f.Dismissed[id]
	return ok
}

// Dismiss records a dismissal, reporting whether it changed anything.
func (f *DismissalFile) Dismiss(id string, at time.Time) bool {
	if f.Dismissed == nil {
		f.Dismissed = map[string]time.Time{}
	}
	if _, exists := f.Dismissed[id]; exists {
		return false
	}
	f.Dismissed[id] = at.UTC()
	return true
}

// FilterDismissed drops notices the user has already dismissed.
func FilterDismissed(campaignRoot string, notices []Notice) []Notice {
	f, err := LoadDismissals(campaignRoot)
	if err != nil {
		// An unreadable dismissal file must not silence advisories, and must
		// not fail the command carrying them. Showing a notice the user
		// dismissed is the recoverable direction; hiding one they never did
		// is not.
		return notices
	}
	return f.Filter(notices)
}

// Filter drops notices this dismissal set covers. Separated from the disk load
// so the decision is testable without a filesystem.
func (f *DismissalFile) Filter(notices []Notice) []Notice {
	kept := make([]Notice, 0, len(notices))
	for _, n := range notices {
		if !f.IsDismissed(n.ID) {
			kept = append(kept, n)
		}
	}
	return kept
}
