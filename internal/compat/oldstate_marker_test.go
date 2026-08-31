package compat

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
)

// TestOldStateLinkMarkersResolve reads every .camp shape camp has ever written
// and asserts each still names a campaign. A marker that stops resolving does
// not error: camp simply reports "not inside a camp" from a directory that
// worked yesterday.
func TestOldStateLinkMarkersResolve(t *testing.T) {
	tests := []struct {
		name       string
		fixture    string
		wantID     string
		wantIDs    []string
		wantKind   string
		wantSchema int
	}{
		{
			name:       "v1 legacy campaign_id and campaign_root",
			fixture:    "camp-marker-v1.json",
			wantID:     legacyCampaignID,
			wantIDs:    []string{legacyCampaignID},
			wantKind:   campaign.KindProject,
			wantSchema: 1,
		},
		{
			name:       "v2 active_campaign_id without kind",
			fixture:    "camp-marker-v2.json",
			wantID:     legacyCampaignID,
			wantIDs:    []string{legacyCampaignID},
			wantKind:   campaign.KindProject,
			wantSchema: 2,
		},
		{
			name:       "v3 shared attachment",
			fixture:    "camp-marker-v3-attachment.json",
			wantID:     legacyCampaignID,
			wantIDs:    []string{legacyCampaignID, secondCampaignID},
			wantKind:   campaign.KindAttachment,
			wantSchema: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			marker, err := campaign.ReadMarkerFile(oldStateFixturePath(t, tt.fixture))
			if err != nil {
				t.Fatalf("reading %s: %v", tt.fixture, err)
			}
			if got := marker.EffectiveCampaignID(); got != tt.wantID {
				t.Fatalf("effective campaign id: got %q, want %q", got, tt.wantID)
			}
			if got := marker.EffectiveCampaignIDs(); !equalStrings(got, tt.wantIDs) {
				t.Fatalf("bound campaign ids: got %v, want %v", got, tt.wantIDs)
			}
			if marker.Kind != tt.wantKind {
				t.Fatalf("kind: got %q, want %q", marker.Kind, tt.wantKind)
			}
			if marker.Version != tt.wantSchema {
				t.Fatalf("version: got %d, want %d (old markers keep their own version on read)", marker.Version, tt.wantSchema)
			}
			if !marker.HasCampaign(tt.wantID) {
				t.Fatalf("marker does not report a binding to %s", tt.wantID)
			}
		})
	}
}

// TestOldStateLinkMarkerLegacyFieldsSurviveRead pins the two legacy field names
// specifically, because they are the ones a rename pass would treat as dead
// weight. They are the only binding a v1 marker carries.
func TestOldStateLinkMarkerLegacyFieldsSurviveRead(t *testing.T) {
	marker, err := campaign.ReadMarkerFile(oldStateFixturePath(t, "camp-marker-v1.json"))
	if err != nil {
		t.Fatalf("reading v1 marker: %v", err)
	}
	if marker.CampaignID != legacyCampaignID {
		t.Fatalf("campaign_id: got %q, want %q", marker.CampaignID, legacyCampaignID)
	}
	if marker.CampaignRoot != "/home/dev/campaigns/legacy-campaign" {
		t.Fatalf("campaign_root: got %q", marker.CampaignRoot)
	}
	if marker.ProjectName != "guild-core" {
		t.Fatalf("project_name: got %q", marker.ProjectName)
	}
}

// TestLinkMarkerWrittenKeysAreFrozen pins the keys camp writes today. The
// Festival app and the obey daemon read this file, so its key names are a wire
// format even though it lives on local disk.
//
// The marker is serialized rather than written, because this package touches no
// filesystem. project_name is only produced by this path now — no command still
// writes it — so serialization is the only place left that can pin it. That the
// real writer emits the rest of the set onto a real marker is pinned in
// TestCompatLinkMarkerWrittenKeysAreFrozen
// (tests/integration/compat_oldstate_test.go).
func TestLinkMarkerWrittenKeysAreFrozen(t *testing.T) {
	got := mustJSON(t, campaign.LinkMarker{
		Version:          campaign.LinkMarkerVersion,
		Kind:             campaign.KindAttachment,
		ActiveCampaignID: legacyCampaignID,
		CampaignIDs:      []string{secondCampaignID},
		ProjectName:      "guild-core",
	})

	for _, key := range []string{"version", "kind", "active_campaign_id", "campaign_ids", "project_name"} {
		if _, ok := got[key]; !ok {
			t.Errorf(".camp marker lost the %q key", key)
		}
	}
	if got["version"] != float64(campaign.LinkMarkerVersion) {
		t.Errorf("written marker version: got %v, want %d", got["version"], campaign.LinkMarkerVersion)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
