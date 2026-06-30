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
			want: "[OBEY-CAMPAIGN-8deed8b4-qst_abc-FE-CW0003-WI-WI-abcdef] full",
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
	want := "[obey-campaign:8deed8b4-qst_abc-FE-CW0003-WI-WI-abcdef] full"
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
