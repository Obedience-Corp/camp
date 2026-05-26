package git

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestFormatContextTagsFull_AllCombinations(t *testing.T) {
	cases := []struct {
		name                                  string
		campaign, quest, fest, workitem, want string
	}{
		{
			name:     "empty campaign returns empty",
			campaign: "", quest: "qst_abc", fest: "CW0003", workitem: "WI-abcdef",
			want: "",
		},
		{
			name:     "campaign only",
			campaign: "8deed8b4",
			want:     "[OBEY-CAMPAIGN-8deed8b4]",
		},
		{
			name:     "campaign + quest",
			campaign: "8deed8b4", quest: "qst_abc",
			want: "[OBEY-CAMPAIGN-8deed8b4-qst_abc]",
		},
		{
			name:     "campaign + festival",
			campaign: "8deed8b4", fest: "CW0003",
			want: "[OBEY-CAMPAIGN-8deed8b4-FE-CW0003]",
		},
		{
			name:     "campaign + workitem",
			campaign: "8deed8b4", workitem: "WI-abcdef",
			want: "[OBEY-CAMPAIGN-8deed8b4-WI-WI-abcdef]",
		},
		{
			name:     "campaign + quest + festival",
			campaign: "8deed8b4", quest: "qst_abc", fest: "CW0003",
			want: "[OBEY-CAMPAIGN-8deed8b4-qst_abc-FE-CW0003]",
		},
		{
			name:     "campaign + quest + workitem",
			campaign: "8deed8b4", quest: "qst_abc", workitem: "WI-abcdef",
			want: "[OBEY-CAMPAIGN-8deed8b4-qst_abc-WI-WI-abcdef]",
		},
		{
			name:     "campaign + festival + workitem",
			campaign: "8deed8b4", fest: "CW0003", workitem: "WI-abcdef",
			want: "[OBEY-CAMPAIGN-8deed8b4-FE-CW0003-WI-WI-abcdef]",
		},
		{
			name:     "all four components",
			campaign: "8deed8b4", quest: "qst_abc", fest: "CW0003", workitem: "WI-abcdef",
			want: "[OBEY-CAMPAIGN-8deed8b4-qst_abc-FE-CW0003-WI-WI-abcdef]",
		},
		{
			name:     "campaign id truncated to 8 chars",
			campaign: "8deed8b4abcdef", workitem: "WI-abcdef",
			want: "[OBEY-CAMPAIGN-8deed8b4-WI-WI-abcdef]",
		},
		{
			name:     "workitem ref normalized",
			campaign: "8deed8b4", workitem: "abcdef",
			want: "[OBEY-CAMPAIGN-8deed8b4-WI-WI-abcdef]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatContextTagsFull(tc.campaign, tc.quest, tc.fest, tc.workitem)
			if got != tc.want {
				t.Fatalf("FormatContextTagsFull = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatCampaignTag_BackwardCompat(t *testing.T) {
	cases := []struct {
		campaign, quest, want string
	}{
		{campaign: "8deed8b4", want: "[OBEY-CAMPAIGN-8deed8b4]"},
		{campaign: "8deed8b4", quest: "qst_abc", want: "[OBEY-CAMPAIGN-8deed8b4-qst_abc]"},
	}
	for _, tc := range cases {
		var got string
		if tc.quest == "" {
			got = FormatCampaignTag(tc.campaign)
		} else {
			got = FormatCampaignTag(tc.campaign, tc.quest)
		}
		if got != tc.want {
			t.Fatalf("FormatCampaignTag back-compat broke: got %q, want %q", got, tc.want)
		}
	}
}

func TestParseTag_KnownCombinations(t *testing.T) {
	cases := []struct {
		subject                                         string
		wantCampaign, wantQuest, wantFest, wantWorkitem string
	}{
		{
			subject:      "[OBEY-CAMPAIGN-8deed8b4] feat: thing",
			wantCampaign: "8deed8b4",
		},
		{
			subject:      "[OBEY-CAMPAIGN-8deed8b4-qst_abc] message",
			wantCampaign: "8deed8b4", wantQuest: "qst_abc",
		},
		{
			subject:      "[OBEY-CAMPAIGN-8deed8b4-FE-CW0003] feat: ...",
			wantCampaign: "8deed8b4", wantFest: "CW0003",
		},
		{
			subject:      "[OBEY-CAMPAIGN-8deed8b4-WI-WI-abcdef] x",
			wantCampaign: "8deed8b4", wantWorkitem: "WI-abcdef",
		},
		{
			subject:      "[OBEY-CAMPAIGN-8deed8b4-qst_abc-FE-CW0003-WI-WI-abcdef] all",
			wantCampaign: "8deed8b4", wantQuest: "qst_abc", wantFest: "CW0003", wantWorkitem: "WI-abcdef",
		},
		{
			subject:      "no tag here at all",
			wantCampaign: "",
		},
		{
			subject:      `Revert "[OBEY-CAMPAIGN-8deed8b4-WI-WI-fake01] feat: x"`,
			wantCampaign: "",
		},
		{
			subject:      `chore: backport "[OBEY-CAMPAIGN-8deed8b4-FE-CW0003] feat: y" from main`,
			wantCampaign: "",
		},
		{
			subject:      "[OBEY-CAMPAIGN-8deed8b4-BOGUS-WI-WI-abcdef] x",
			wantCampaign: "8deed8b4", wantWorkitem: "WI-abcdef",
		},
	}
	for _, tc := range cases {
		t.Run(tc.subject, func(t *testing.T) {
			got := ParseTag(tc.subject)
			if got.CampaignID != tc.wantCampaign {
				t.Fatalf("CampaignID = %q, want %q", got.CampaignID, tc.wantCampaign)
			}
			if got.QuestID != tc.wantQuest {
				t.Fatalf("QuestID = %q, want %q", got.QuestID, tc.wantQuest)
			}
			if got.FestRef != tc.wantFest {
				t.Fatalf("FestRef = %q, want %q", got.FestRef, tc.wantFest)
			}
			if got.WorkitemRef != tc.wantWorkitem {
				t.Fatalf("WorkitemRef = %q, want %q", got.WorkitemRef, tc.wantWorkitem)
			}
		})
	}
}

func TestParseTag_RoundTripProperty(t *testing.T) {
	const iterations = 100
	for i := 0; i < iterations; i++ {
		campaign := randHex(t, 4)
		quest := ""
		fest := ""
		workitem := ""
		if i%2 == 0 {
			quest = "qst_" + randHex(t, 3)
		}
		if i%3 == 0 {
			fest = "CW" + randHex(t, 2)
		}
		if i%5 == 0 {
			workitem = "WI-" + randHex(t, 3)
		}

		tag := FormatContextTagsFull(campaign, quest, fest, workitem)
		got := ParseTag(tag)
		if got.CampaignID != campaign {
			t.Fatalf("iter %d: campaign round-trip broke: %q -> %q", i, campaign, got.CampaignID)
		}
		if got.QuestID != quest {
			t.Fatalf("iter %d: quest round-trip broke: %q -> %q (tag %q)", i, quest, got.QuestID, tag)
		}
		if got.FestRef != fest {
			t.Fatalf("iter %d: fest round-trip broke: %q -> %q (tag %q)", i, fest, got.FestRef, tag)
		}
		if got.WorkitemRef != workitem {
			t.Fatalf("iter %d: workitem round-trip broke: %q -> %q (tag %q)", i, workitem, got.WorkitemRef, tag)
		}
	}
}

func TestParseTag_AnchoringAdversarial(t *testing.T) {
	cases := []struct {
		name       string
		subject    string
		wantParsed bool
		wantID     string
		wantWIRef  string
	}{
		{name: "happy path leading tag", subject: "[OBEY-CAMPAIGN-abcd1234-WI-WI-deadbe] feat: X", wantParsed: true, wantID: "abcd1234", wantWIRef: "WI-deadbe"},
		{name: "revert subject", subject: `Revert "[OBEY-CAMPAIGN-abcd1234] feat: X"`, wantParsed: false},
		{name: "leading whitespace", subject: " [OBEY-CAMPAIGN-abcd1234] x", wantParsed: false},
		{name: "embedded mid-subject", subject: "fix: tag was [OBEY-CAMPAIGN-abcd1234] in old log", wantParsed: false},
		{name: "tag inside backticks", subject: "docs: example `[OBEY-CAMPAIGN-abcd1234]`", wantParsed: false},
		{name: "two tags only the leading wins", subject: "[OBEY-CAMPAIGN-aaaaaaaa] body [OBEY-CAMPAIGN-bbbbbbbb]", wantParsed: true, wantID: "aaaaaaaa"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseTag(tc.subject)
			if tc.wantParsed {
				if got.CampaignID != tc.wantID {
					t.Fatalf("CampaignID: want %q, got %q", tc.wantID, got.CampaignID)
				}
				if tc.wantWIRef != "" && got.WorkitemRef != tc.wantWIRef {
					t.Fatalf("WorkitemRef: want %q, got %q", tc.wantWIRef, got.WorkitemRef)
				}
			} else if got != (TagComponents{}) {
				t.Fatalf("expected zero-value TagComponents for non-leading tag, got %+v", got)
			}
		})
	}
}

func TestTagBodyScanRegex_FindsEmbedded(t *testing.T) {
	subject := "body has [OBEY-CAMPAIGN-aaa] and [OBEY-CAMPAIGN-bbb]"
	matches := tagBodyScanRegex.FindAllString(subject, -1)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches from body-scan regex, got %d: %v", len(matches), matches)
	}
	want := []string{"[OBEY-CAMPAIGN-aaa]", "[OBEY-CAMPAIGN-bbb]"}
	for i, m := range matches {
		if m != want[i] {
			t.Errorf("match[%d] = %q, want %q", i, m, want[i])
		}
	}
}

func randHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
