package compat

import (
	"testing"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
)

// TestFrozenCampaignScopeType is a compile-time pin on the exported scope type
// and its fields. A rename would compile everywhere inside camp and break only
// the Go API that out-of-tree tooling builds against.
func TestFrozenCampaignScopeType(t *testing.T) {
	scope := cmdutil.CampaignScope{Org: "obedience", Status: "active", All: false}
	if scope.Org != "obedience" || scope.Status != "active" || scope.All {
		t.Fatalf("CampaignScope fields changed meaning: %+v", scope)
	}
}

// TestFrozenSwitchSelectorGrammar pins org/campaign[@tab]. The selector is
// typed by hand, lives in shell aliases, and is completed by the installed
// shell integration, so its shape is a user contract in all three places.
func TestFrozenSwitchSelectorGrammar(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		org      string
		campaign string
		tab      string
		hasTab   bool
	}{
		{name: "bare campaign", raw: "legacy-campaign", campaign: "legacy-campaign"},
		{name: "org qualified", raw: "obedience/legacy-campaign", org: "obedience", campaign: "legacy-campaign"},
		{name: "tab", raw: "legacy-campaign@projects", campaign: "legacy-campaign", tab: "projects", hasTab: true},
		{
			name:     "org and tab",
			raw:      "obedience/legacy-campaign@festivals",
			org:      "obedience",
			campaign: "legacy-campaign",
			tab:      "festivals",
			hasTab:   true,
		},
		{name: "empty tab is still a tab", raw: "legacy-campaign@", campaign: "legacy-campaign", hasTab: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cmdutil.ParseSwitchSelector(tt.raw)
			if got.Org != tt.org || got.Campaign != tt.campaign || got.Tab != tt.tab || got.HasTab != tt.hasTab {
				t.Fatalf("selector %q parsed as %+v; want org=%q campaign=%q tab=%q hasTab=%v",
					tt.raw, got, tt.org, tt.campaign, tt.tab, tt.hasTab)
			}
		})
	}
}

// TestFrozenMachineSelectorGrammar pins machine:campaign, including the rule
// that keeps it collision-free with the org and tab dimensions.
func TestFrozenMachineSelectorGrammar(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		machine   string
		remainder string
		org       string
		campaign  string
		tab       string
	}{
		{name: "machine and campaign", raw: "workstation:legacy-campaign", machine: "workstation", remainder: "legacy-campaign", campaign: "legacy-campaign"},
		{
			name:      "machine, org, campaign, and tab",
			raw:       "workstation:obedience/legacy-campaign@projects",
			machine:   "workstation",
			remainder: "obedience/legacy-campaign@projects",
			org:       "obedience",
			campaign:  "legacy-campaign",
			tab:       "projects",
		},
		{name: "no colon stays local", raw: "obedience/legacy-campaign", remainder: "obedience/legacy-campaign", org: "obedience", campaign: "legacy-campaign"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cmdutil.ParseMachineSelector(tt.raw)
			if err != nil {
				t.Fatalf("parsing %q: %v", tt.raw, err)
			}
			if got.Machine != tt.machine || got.Remainder != tt.remainder {
				t.Fatalf("selector %q split as machine=%q remainder=%q; want %q / %q",
					tt.raw, got.Machine, got.Remainder, tt.machine, tt.remainder)
			}
			if got.Sel.Org != tt.org || got.Sel.Campaign != tt.campaign || got.Sel.Tab != tt.tab {
				t.Fatalf("selector %q inner parse %+v; want org=%q campaign=%q tab=%q",
					tt.raw, got.Sel, tt.org, tt.campaign, tt.tab)
			}
		})
	}
}
