package cmdutil

import "testing"

func TestParseMachineSelectorGrammar(t *testing.T) {
	cases := []struct {
		in       string
		machine  string
		org      string
		campaign string
		tab      string
		hasTab   bool
	}{
		{"machine:campaign", "machine", "", "campaign", "", false},
		{"machine:org/campaign", "machine", "org", "campaign", "", false},
		{"machine:org/campaign@f", "machine", "org", "campaign", "f", true},
		{"local:campaign", "local", "", "campaign", "", false},
		{"campaign", "", "", "campaign", "", false},
		{"org/campaign", "", "org", "campaign", "", false},
	}
	for _, tc := range cases {
		got, err := ParseMachineSelector(tc.in)
		if err != nil {
			t.Fatalf("ParseMachineSelector(%q): unexpected error %v", tc.in, err)
		}
		if got.Machine != tc.machine {
			t.Errorf("%q: Machine = %q, want %q", tc.in, got.Machine, tc.machine)
		}
		if got.Sel.Org != tc.org || got.Sel.Campaign != tc.campaign ||
			got.Sel.Tab != tc.tab || got.Sel.HasTab != tc.hasTab {
			t.Errorf("%q: Sel = %+v, want org=%q campaign=%q tab=%q hasTab=%v",
				tc.in, got.Sel, tc.org, tc.campaign, tc.tab, tc.hasTab)
		}
	}
}

func TestParseMachineSelectorNoColonByteIdentical(t *testing.T) {
	for _, raw := range []string{"campaign", "org/campaign", "org/campaign@f", "campaign@f"} {
		got, err := ParseMachineSelector(raw)
		if err != nil {
			t.Fatalf("ParseMachineSelector(%q): %v", raw, err)
		}
		if got.Machine != "" {
			t.Errorf("%q: Machine = %q, want empty", raw, got.Machine)
		}
		if want := ParseSwitchSelector(raw); got.Sel != want {
			t.Errorf("%q: Sel = %+v, want byte-identical %+v", raw, got.Sel, want)
		}
		if got.Remainder != raw {
			t.Errorf("%q: Remainder = %q, want %q", raw, got.Remainder, raw)
		}
	}
}

func TestParseMachineSelectorRejections(t *testing.T) {
	cases := map[string]string{
		"empty id before colon": ":campaign",
		"slash in id":           "mach/ine:campaign",
		"at in id":              "mach@ine:campaign",
		"invalid name (upper)":  "Box:campaign",
		"invalid name (digit)":  "1box:campaign",
	}
	for why, in := range cases {
		if _, err := ParseMachineSelector(in); err == nil {
			t.Errorf("ParseMachineSelector(%q) = nil error, want rejection (%s)", in, why)
		}
	}
}
