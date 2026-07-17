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
const LinkMarkerVersion = 4

// Marker kinds. KindProject is the original linked-project shape;
// KindAttachment is a non-project directory attached to a campaign so context
// detection works from inside it without registering a project.
const (
	KindProject    = "project"
	KindAttachment = "attachment"
)

// LinkMarker records the campaign context for a linked directory.
// A Kind="project" marker is the original linked-project shape and is paired
// with a projects/<name> symlink and a registry entry. A Kind="attachment"
// marker is a lighter binding for arbitrary directories the user has already
// symlinked into the campaign. Attachments may be referenced by multiple
// campaigns; ActiveCampaignID remains the fallback for direct access that does
// not carry a campaign-local symlink path.
type LinkMarker struct {
	Version          int      `json:"version"`
	Kind             string   `json:"kind,omitempty"`
	ActiveCampaignID string   `json:"active_campaign_id,omitempty"`
	CampaignIDs      []string `json:"campaign_ids,omitempty"`

	// Project-only; ignored when Kind="attachment".
	ProjectName string `json:"project_name,omitempty"`

	// Legacy fields kept for backward-compatible reads only.
	CampaignID   string `json:"campaign_id,omitempty"`
	CampaignRoot string `json:"campaign_root,omitempty"`
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
	if marker.ActiveCampaignID == "" && len(marker.CampaignIDs) > 0 {
		marker.ActiveCampaignID = marker.CampaignIDs[0]
	}
	// Pre-V3 markers had no Kind field. Default to KindProject so existing
	// linked-project markers continue to behave exactly as before.
	if marker.Kind == "" {
		marker.Kind = KindProject
	}

	return &marker, nil
}

// WriteMarker writes the link marker for a linked project directory.
func WriteMarker(projectDir string, marker LinkMarker) error {
	if marker.Version < LinkMarkerVersion {
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
	if m.CampaignID != "" {
		return m.CampaignID
	}
	if len(m.CampaignIDs) > 0 {
		return m.CampaignIDs[0]
	}
	return ""
}

// EffectiveCampaignIDs returns all campaign IDs bound to the marker in stable
// order, de-duplicated across current and legacy fields.
func (m LinkMarker) EffectiveCampaignIDs() []string {
	ids := make([]string, 0, len(m.CampaignIDs)+2)
	seen := make(map[string]struct{}, cap(ids))
	add := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	add(m.ActiveCampaignID)
	add(m.CampaignID)
	for _, id := range m.CampaignIDs {
		add(id)
	}
	return ids
}

// HasCampaign reports whether the marker is bound to campaignID.
func (m LinkMarker) HasCampaign(campaignID string) bool {
	for _, id := range m.EffectiveCampaignIDs() {
		if id == campaignID {
			return true
		}
	}
	return false
}

// AddCampaign adds campaignID to an attachment marker. The first campaign
// remains the active fallback for direct access; additional campaigns are
// selected from their campaign-local symlink paths during detection.
func (m *LinkMarker) AddCampaign(campaignID string) bool {
	if m == nil || campaignID == "" || m.HasCampaign(campaignID) {
		return false
	}
	if m.ActiveCampaignID == "" && m.CampaignID == "" {
		m.ActiveCampaignID = campaignID
		return true
	}
	m.CampaignIDs = append(m.CampaignIDs, campaignID)
	return true
}

// RemoveCampaign removes campaignID from an attachment marker while keeping
// the remaining campaigns and their stable fallback order.
func (m *LinkMarker) RemoveCampaign(campaignID string) bool {
	if m == nil || !m.HasCampaign(campaignID) {
		return false
	}

	remaining := make([]string, 0, len(m.EffectiveCampaignIDs()))
	for _, id := range m.EffectiveCampaignIDs() {
		if id != campaignID {
			remaining = append(remaining, id)
		}
	}

	m.ActiveCampaignID = ""
	m.CampaignID = ""
	m.CampaignIDs = nil
	if len(remaining) > 0 {
		m.ActiveCampaignID = remaining[0]
		if len(remaining) > 1 {
			m.CampaignIDs = append(m.CampaignIDs, remaining[1:]...)
		}
	}
	return true
}
