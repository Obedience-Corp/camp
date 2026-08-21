package commitkit_test

import (
	"testing"

	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

func TestPrependContextTagsFull_Legacy(t *testing.T) {
	cases := []struct {
		name                                       string
		campaign, quest, fest, workitem, msg, want string
	}{
		{
			name:     "no campaign returns message unchanged",
			campaign: "", quest: "qst_x", fest: "CW0003", workitem: "WI-abcdef",
			msg: "hello", want: "hello",
		},
		{
			name:     "id only emits legacy marker",
			campaign: "8deed8b4", quest: "qst_abc", fest: "CW0003", workitem: "WI-abcdef",
			msg:  "full",
			want: "[OBEY-CAMPAIGN-8deed8b4-qst_abc-FE-CW0003-WI-abcdef] full",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := commitkit.PrependContextTagsFull(tc.campaign, tc.quest, tc.fest, tc.workitem, tc.msg)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrependContextTagsFullNamed(t *testing.T) {
	got := commitkit.PrependContextTagsFullNamed("obey-campaign", "8deed8b4", "qst_abc", "CW0003", "WI-abcdef", "full")
	want := "[obey-campaign:8deed8b4-qst_abc-FE-CW0003-WI-abcdef] full"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCommitkit_ParseTag_RoundTrip(t *testing.T) {
	tag := commitkit.PrependContextTagsFullNamed("obey-campaign", "8deed8b4", "qst_abc", "CW0003", "WI-abcdef", "subject")
	got := commitkit.ParseTag(tag)
	if got.CampaignName != "obey-campaign" || got.CampaignID != "8deed8b4" || got.QuestID != "qst_abc" || got.FestRef != "CW0003" || got.WorkitemRef != "WI-abcdef" {
		t.Fatalf("parse round-trip broke: %#v", got)
	}
}

func TestCommitkit_ParseTagDetailed_ReExport(t *testing.T) {
	subject := "[OBEY-CAMPAIGN-abc-WI-WI-ZZZZ-extra-junk] x"
	got, warnings := commitkit.ParseTagDetailed(subject)
	if got.CampaignID != "abc" {
		t.Errorf("CampaignID = %q, want abc", got.CampaignID)
	}
	if got.WorkitemRef != "" {
		t.Errorf("WorkitemRef should be zeroed on shape failure, got %q", got.WorkitemRef)
	}
	if len(warnings) == 0 {
		t.Fatal("expected at least one warning from degraded parse")
	}
	if warnings[0].Field != "workitem_ref" {
		t.Errorf("warning[0].Field = %q, want workitem_ref", warnings[0].Field)
	}
}

func TestCommitkit_FormatTag_PhaseAndSequence(t *testing.T) {
	cases := []struct {
		name string
		tc   commitkit.TagComponents
		want string
	}{
		{
			name: "no campaign id emits nothing",
			tc:   commitkit.TagComponents{FestRef: "CC0008", Phase: "001", Sequence: "02"},
			want: "",
		},
		{
			name: "sequence without phase is dropped",
			tc: commitkit.TagComponents{
				CampaignName: "obey-campaign", CampaignID: "8deed8b4",
				FestRef: "CC0008", Sequence: "02",
			},
			want: "[obey-campaign:8deed8b4-FE-CC0008]",
		},
		{
			name: "phase without festival is dropped",
			tc: commitkit.TagComponents{
				CampaignName: "obey-campaign", CampaignID: "8deed8b4", Phase: "001",
			},
			want: "[obey-campaign:8deed8b4]",
		},
		{
			name: "festival phase and sequence",
			tc: commitkit.TagComponents{
				CampaignName: "obey-campaign", CampaignID: "8deed8b4",
				FestRef: "CC0008", Phase: "001", Sequence: "02",
			},
			want: "[obey-campaign:8deed8b4-FE-CC0008-PH-001-SQ-02]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commitkit.FormatTag(tc.tc); got != tc.want {
				t.Fatalf("FormatTag = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCommitkit_FormatTag_ParseRoundTrip(t *testing.T) {
	want := commitkit.TagComponents{
		CampaignName: "obey-campaign", CampaignID: "8deed8b4", QuestID: "qst_abc",
		FestRef: "CC0008", Phase: "001", Sequence: "02",
		WorkitemRef: "WI-abcdef", NoteRef: "NT-123456",
	}
	tag := commitkit.FormatTag(want)
	got := commitkit.ParseTag(tag + " feat: update camp scaffold")
	if got != want {
		t.Fatalf("round trip of %q = %#v, want %#v", tag, got, want)
	}
}

func TestCommitkit_ValidateTagComponents(t *testing.T) {
	cases := []struct {
		name    string
		tc      commitkit.TagComponents
		wantErr bool
	}{
		{
			name:    "no campaign id",
			tc:      commitkit.TagComponents{FestRef: "CC0008", Phase: "001"},
			wantErr: true,
		},
		{
			name:    "phase without festival would be dropped",
			tc:      commitkit.TagComponents{CampaignID: "8deed8b4", Phase: "001"},
			wantErr: true,
		},
		{
			name:    "sequence without phase would be dropped",
			tc:      commitkit.TagComponents{CampaignID: "8deed8b4", FestRef: "CC0008", Sequence: "02"},
			wantErr: true,
		},
		{
			name:    "phase carrying a directory suffix would be dropped",
			tc:      commitkit.TagComponents{CampaignID: "8deed8b4", FestRef: "CC0008", Phase: "001_IMPLEMENT"},
			wantErr: true,
		},
		{
			name: "fully anchored components validate clean",
			tc: commitkit.TagComponents{
				CampaignName: "obey-campaign", CampaignID: "8deed8b4",
				FestRef: "CC0008", Phase: "001", Sequence: "02",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := commitkit.ValidateTagComponents(tc.tc)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateTagComponents(%+v) = nil, want an error", tc.tc)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateTagComponents(%+v) = %v, want nil", tc.tc, err)
			}
		})
	}
}

func TestCommitkit_FormatTag_DropsWhatValidateRejects(t *testing.T) {
	// The two halves of the documented contract: FormatTag stays lenient and
	// drops the malformed phase, and ValidateTagComponents is the signal that
	// it did.
	tc := commitkit.TagComponents{
		CampaignName: "obey-campaign", CampaignID: "8deed8b4",
		FestRef: "CC0008", Phase: "001_IMPLEMENT", Sequence: "02",
	}
	if err := commitkit.ValidateTagComponents(tc); err == nil {
		t.Fatal("ValidateTagComponents = nil, want an error naming the phase")
	}
	got := commitkit.FormatTag(tc)
	want := "[obey-campaign:8deed8b4-FE-CC0008]"
	if got != want {
		t.Fatalf("FormatTag = %q, want %q", got, want)
	}
	if _, warnings := commitkit.ParseTagDetailed(got + " x"); len(warnings) != 0 {
		t.Fatalf("emitted tag %q reparsed with warnings: %+v", got, warnings)
	}
}
