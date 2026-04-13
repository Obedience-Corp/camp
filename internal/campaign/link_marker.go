package campaign

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// LinkMarkerFile is the marker written into linked project roots so camp can
// recover campaign context from the resolved external cwd.
const LinkMarkerFile = ".camp"

// LinkMarkerVersion is the current .camp schema version.
const LinkMarkerVersion = 2

// LinkMarker records the active campaign context for a linked project.
type LinkMarker struct {
	Version          int    `json:"version"`
	ActiveCampaignID string `json:"active_campaign_id,omitempty"`

	// Legacy fields kept for backward-compatible reads only.
	CampaignID   string `json:"campaign_id,omitempty"`
	CampaignRoot string `json:"campaign_root,omitempty"`
	ProjectName  string `json:"project_name,omitempty"`
}

// MarkerPath returns the marker file path for a linked project directory.
func MarkerPath(projectDir string) string {
	return filepath.Join(projectDir, LinkMarkerFile)
}

// ReadMarker reads a link marker from a project directory.
func ReadMarker(projectDir string) (*LinkMarker, error) {
	return ReadMarkerFile(MarkerPath(projectDir))
}

// ReadMarkerFile reads a link marker from an explicit file path.
func ReadMarkerFile(path string) (*LinkMarker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var marker LinkMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return nil, err
	}

	if marker.Version == 0 {
		marker.Version = 1
	}
	if marker.ActiveCampaignID == "" && marker.CampaignID != "" {
		marker.ActiveCampaignID = marker.CampaignID
	}

	return &marker, nil
}

// WriteMarker writes the link marker for a linked project directory.
func WriteMarker(projectDir string, marker LinkMarker) error {
	if marker.Version == 0 {
		marker.Version = LinkMarkerVersion
	}

	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return fsutil.WriteFileAtomically(MarkerPath(projectDir), data, 0644)
}

// RemoveMarker removes the link marker from a linked project directory.
func RemoveMarker(projectDir string) error {
	err := os.Remove(MarkerPath(projectDir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// EffectiveCampaignID returns the campaign ID that should be used for context
// resolution, including compatibility with legacy markers.
func (m LinkMarker) EffectiveCampaignID() string {
	if m.ActiveCampaignID != "" {
		return m.ActiveCampaignID
	}
	return m.CampaignID
}
