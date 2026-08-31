package compat

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

// TestHistoricalCommitTagsParse pins the commit subject grammars camp must keep
// reading. Every one of these forms exists in shipped repository history, so
// `camp workitem commits` and the audit backfill stop finding real commits the
// moment a parser stops recognizing one.
func TestHistoricalCommitTagsParse(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		want    git.TagComponents
	}{
		{
			name:    "name-style head",
			subject: "[obey-campaign:8deed8b4] docs: terminology contract",
			want:    git.TagComponents{CampaignID: "8deed8b4", CampaignName: "obey-campaign"},
		},
		{
			name:    "legacy id-only head",
			subject: "[OBEY-CAMPAIGN-8deed8b4] Create: thing",
			want:    git.TagComponents{CampaignID: "8deed8b4"},
		},
		{
			name:    "festival, phase, and sequence segments",
			subject: "[obey-campaign:8deed8b4-FE-CC0008-PH-002-SQ-003] fest: pin fixtures",
			want: git.TagComponents{
				CampaignID:   "8deed8b4",
				CampaignName: "obey-campaign",
				FestRef:      "CC0008",
				Phase:        "002",
				Sequence:     "003",
			},
		},
		{
			name:    "ritual festival ref keeps its RI- marker",
			subject: "[OBEY-CAMPAIGN-8deed8b4-FE-RI-XX0001] ritual: weekly",
			want:    git.TagComponents{CampaignID: "8deed8b4", FestRef: "RI-XX0001"},
		},
		{
			name:    "quest and workitem segments",
			subject: "[obey-campaign:8deed8b4-qst_abc-WI-25121c] feat: ledger path",
			want: git.TagComponents{
				CampaignID:   "8deed8b4",
				CampaignName: "obey-campaign",
				QuestID:      "qst_abc",
				WorkitemRef:  "WI-25121c",
			},
		},
		{
			name:    "historical doubled workitem ref normalizes",
			subject: "[OBEY-CAMPAIGN-8deed8b4-WI-WI-25121c] feat: backfill miss",
			want:    git.TagComponents{CampaignID: "8deed8b4", WorkitemRef: "WI-25121c"},
		},
		{
			name:    "note segment",
			subject: "[obey-campaign:8deed8b4-NT-abc123] note: meeting",
			want: git.TagComponents{
				CampaignID:   "8deed8b4",
				CampaignName: "obey-campaign",
				NoteRef:      "NT-abc123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := git.ParseTag(tt.subject); got != tt.want {
				t.Fatalf("parsing a historical commit subject:\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// TestLegacyTagEmitterStillProducesItsOwnFormat pins the fallback head. camp
// emits it whenever a campaign name has no slug, and every commit written that
// way is already in history.
func TestLegacyTagEmitterStillProducesItsOwnFormat(t *testing.T) {
	got := commitkit.FormatCampaignTag("8deed8b4")
	if got != "[OBEY-CAMPAIGN-8deed8b4]" {
		t.Fatalf("legacy tag: got %q, want %q", got, "[OBEY-CAMPAIGN-8deed8b4]")
	}
	if parsed := git.ParseTag(got + " chore: round trip"); parsed.CampaignID != "8deed8b4" {
		t.Fatalf("the legacy emitter and parser disagree: %+v", parsed)
	}
}

// TestTagComponentsJSONKeysAreFrozen pins the serialized parse result, which
// agents consume when they ask camp what a commit belongs to.
func TestTagComponentsJSONKeysAreFrozen(t *testing.T) {
	got := mustJSON(t, git.TagComponents{
		CampaignID:   "8deed8b4",
		CampaignName: "obey-campaign",
		QuestID:      "qst_abc",
		FestRef:      "CC0008",
		Phase:        "002",
		Sequence:     "003",
		WorkitemRef:  "WI-25121c",
		NoteRef:      "NT-abc123",
	})

	for _, key := range []string{
		"campaign_id", "campaign_name", "quest_id",
		"fest_ref", "phase", "sequence", "workitem_ref", "note_ref",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("parsed tag lost the %q key", key)
		}
	}
}
