package git

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestFormatContextTagsFull_AllCombinations(t *testing.T) {
	cases := []struct {
		name                                         string
		cname, campaign, quest, fest, workitem, want string
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
			want: "[OBEY-CAMPAIGN-8deed8b4-WI-abcdef]",
		},
		{
			name:     "campaign + quest + festival",
			campaign: "8deed8b4", quest: "qst_abc", fest: "CW0003",
			want: "[OBEY-CAMPAIGN-8deed8b4-qst_abc-FE-CW0003]",
		},
		{
			name:     "campaign + quest + workitem",
			campaign: "8deed8b4", quest: "qst_abc", workitem: "WI-abcdef",
			want: "[OBEY-CAMPAIGN-8deed8b4-qst_abc-WI-abcdef]",
		},
		{
			name:     "campaign + festival + workitem",
			campaign: "8deed8b4", fest: "CW0003", workitem: "WI-abcdef",
			want: "[OBEY-CAMPAIGN-8deed8b4-FE-CW0003-WI-abcdef]",
		},
		{
			name:     "all four components",
			campaign: "8deed8b4", quest: "qst_abc", fest: "CW0003", workitem: "WI-abcdef",
			want: "[OBEY-CAMPAIGN-8deed8b4-qst_abc-FE-CW0003-WI-abcdef]",
		},
		{
			name:     "campaign id truncated to 8 chars",
			campaign: "8deed8b4abcdef", workitem: "WI-abcdef",
			want: "[OBEY-CAMPAIGN-8deed8b4-WI-abcdef]",
		},
		{
			name:     "workitem ref normalized",
			campaign: "8deed8b4", workitem: "abcdef",
			want: "[OBEY-CAMPAIGN-8deed8b4-WI-abcdef]",
		},
		{
			name:  "name only",
			cname: "obey-campaign", campaign: "8deed8b4",
			want: "[obey-campaign:8deed8b4]",
		},
		{
			name:  "name + all components",
			cname: "obey-campaign", campaign: "8deed8b4", quest: "qst_abc", fest: "CW0003", workitem: "WI-abcdef",
			want: "[obey-campaign:8deed8b4-qst_abc-FE-CW0003-WI-abcdef]",
		},
		{
			name:  "name slugified (spaces and case)",
			cname: "Brainshare Planning", campaign: "8deed8b4",
			want: "[brainshare-planning:8deed8b4]",
		},
		{
			name:  "unslugifiable name falls back to legacy marker",
			cname: "!!!", campaign: "8deed8b4",
			want: "[OBEY-CAMPAIGN-8deed8b4]",
		},
		{
			name:  "non-hex id falls back to legacy marker",
			cname: "obey-campaign", campaign: "zzzzzzzz",
			want: "[OBEY-CAMPAIGN-zzzzzzzz]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatContextTagsFull(tc.cname, tc.campaign, tc.quest, tc.fest, tc.workitem)
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
		subject                                                   string
		wantCampaign, wantName, wantQuest, wantFest, wantWorkitem string
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
			subject:      "[obey-campaign:8deed8b4] feat: thing",
			wantCampaign: "8deed8b4", wantName: "obey-campaign",
		},
		{
			subject:      "[shrapnel:8deed8b4-qst_abc-FE-CW0003-WI-WI-abcdef] all",
			wantCampaign: "8deed8b4", wantName: "shrapnel", wantQuest: "qst_abc", wantFest: "CW0003", wantWorkitem: "WI-abcdef",
		},
		{
			subject:      "[brainshare-planning:8deed8b4-WI-WI-abcdef] x",
			wantCampaign: "8deed8b4", wantName: "brainshare-planning", wantWorkitem: "WI-abcdef",
		},
		{
			subject:      "[OBEY-CAMPAIGN-8deed8b4-WI-abcdef] single-prefix legacy",
			wantCampaign: "8deed8b4", wantWorkitem: "WI-abcdef",
		},
		{
			subject:      "[obey-campaign:8deed8b4-WI-abcdef] single-prefix name",
			wantCampaign: "8deed8b4", wantName: "obey-campaign", wantWorkitem: "WI-abcdef",
		},
		{
			subject:      "[obey-campaign:8deed8b4-qst_abc-FE-CW0003-WI-abcdef] all single",
			wantCampaign: "8deed8b4", wantName: "obey-campaign", wantQuest: "qst_abc", wantFest: "CW0003", wantWorkitem: "WI-abcdef",
		},
		{
			subject:      "no tag here at all",
			wantCampaign: "",
		},
		{
			subject:      "[wip] feat: thing",
			wantCampaign: "",
		},
		{
			subject:      "[scope:msg] not a campaign tag",
			wantCampaign: "",
		},
		{
			subject:      `Revert "[OBEY-CAMPAIGN-8deed8b4-WI-WI-fake01] feat: x"`,
			wantCampaign: "",
		},
		{
			subject:      `Revert "[obey-campaign:8deed8b4] feat: x"`,
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
			if got.CampaignName != tc.wantName {
				t.Fatalf("CampaignName = %q, want %q", got.CampaignName, tc.wantName)
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
		cname := "c" + randHex(t, 2)
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

		tag := FormatContextTagsFull(cname, campaign, quest, fest, workitem)
		got := ParseTag(tag)
		if got.CampaignName != cname {
			t.Fatalf("iter %d: name round-trip broke: %q -> %q (tag %q)", i, cname, got.CampaignName, tag)
		}
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
		{name: "happy path leading legacy tag", subject: "[OBEY-CAMPAIGN-abcd1234-WI-WI-deadbe] feat: X", wantParsed: true, wantID: "abcd1234", wantWIRef: "WI-deadbe"},
		{name: "happy path leading name tag", subject: "[obey-campaign:abcd1234-WI-WI-deadbe] feat: X", wantParsed: true, wantID: "abcd1234", wantWIRef: "WI-deadbe"},
		{name: "single-prefix name tag", subject: "[obey-campaign:abcd1234-WI-deadbe] feat: X", wantParsed: true, wantID: "abcd1234", wantWIRef: "WI-deadbe"},
		{name: "revert legacy subject", subject: `Revert "[OBEY-CAMPAIGN-abcd1234] feat: X"`, wantParsed: false},
		{name: "revert name subject", subject: `Revert "[obey-campaign:abcd1234] feat: X"`, wantParsed: false},
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

func TestParseTagDetailed_RejectsSilentMerge(t *testing.T) {
	cases := []struct {
		name             string
		subject          string
		wantCampaign     string
		wantName         string
		wantQuest        string
		wantFest         string
		wantWorkitem     string
		wantWarningField []string
	}{
		{
			name:             "duplicate FE second is warned not overwritten",
			subject:          "[OBEY-CAMPAIGN-abc-FE-CW0003-FE-SE0001] x",
			wantCampaign:     "abc",
			wantFest:         "CW0003",
			wantWarningField: []string{"fest_ref"},
		},
		{
			name:             "duplicate quest both fail shape both warned",
			subject:          "[OBEY-CAMPAIGN-abc-qst_xyz-qst_abc] x",
			wantCampaign:     "abc",
			wantQuest:        "qst_xyz",
			wantWarningField: []string{"quest_id"},
		},
		{
			name:             "workitem shape failure zeroes ref",
			subject:          "[OBEY-CAMPAIGN-abc-WI-WI-ZZZZ-extra-junk] x",
			wantCampaign:     "abc",
			wantWarningField: []string{"workitem_ref"},
		},
		{
			name:             "unknown segment then valid WI still recovers",
			subject:          "[OBEY-CAMPAIGN-abc-unknown-WI-WI-aaa111] x",
			wantCampaign:     "abc",
			wantWorkitem:     "WI-aaa111",
			wantWarningField: []string{"unknown"},
		},
		{
			name:             "valid FE then unknown extra then valid WI",
			subject:          "[OBEY-CAMPAIGN-abc-FE-CW0003-extra-WI-WI-aaa111] x",
			wantCampaign:     "abc",
			wantFest:         "CW0003",
			wantWorkitem:     "WI-aaa111",
			wantWarningField: []string{"unknown"},
		},
		{
			name:             "name-style tag warns on duplicate FE",
			subject:          "[obey-campaign:abcdef12-FE-CW0003-FE-SE0001] x",
			wantCampaign:     "abcdef12",
			wantName:         "obey-campaign",
			wantFest:         "CW0003",
			wantWarningField: []string{"fest_ref"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, warnings := ParseTagDetailed(tc.subject)
			if got.CampaignID != tc.wantCampaign {
				t.Errorf("CampaignID = %q, want %q", got.CampaignID, tc.wantCampaign)
			}
			if got.CampaignName != tc.wantName {
				t.Errorf("CampaignName = %q, want %q", got.CampaignName, tc.wantName)
			}
			if got.QuestID != tc.wantQuest {
				t.Errorf("QuestID = %q, want %q", got.QuestID, tc.wantQuest)
			}
			if got.FestRef != tc.wantFest {
				t.Errorf("FestRef = %q, want %q", got.FestRef, tc.wantFest)
			}
			if got.WorkitemRef != tc.wantWorkitem {
				t.Errorf("WorkitemRef = %q, want %q", got.WorkitemRef, tc.wantWorkitem)
			}
			if len(warnings) != len(tc.wantWarningField) {
				t.Fatalf("warnings count = %d, want %d: %+v",
					len(warnings), len(tc.wantWarningField), warnings)
			}
			for i, want := range tc.wantWarningField {
				if warnings[i].Field != want {
					t.Errorf("warning[%d].Field = %q, want %q", i, warnings[i].Field, want)
				}
			}
		})
	}
}

func TestTagBodyScanRegex_FindsEmbedded(t *testing.T) {
	subject := "body has [OBEY-CAMPAIGN-aaa] and [obey-campaign:8deed8b4] tags"
	matches := tagBodyScanRegex.FindAllString(subject, -1)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches from body-scan regex, got %d: %v", len(matches), matches)
	}
	want := []string{"[OBEY-CAMPAIGN-aaa]", "[obey-campaign:8deed8b4]"}
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
