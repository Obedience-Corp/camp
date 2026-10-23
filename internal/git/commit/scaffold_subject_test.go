package commit

import (
	"testing"
	"time"
)

func TestIsScaffoldCommit(t *testing.T) {
	if selectiveRepairSince.IsZero() {
		t.Fatalf("selectiveRepairCutoff %q does not parse", selectiveRepairCutoff)
	}
	after := selectiveRepairSince.Add(time.Hour)
	before := selectiveRepairSince.Add(-time.Hour)
	cases := []struct {
		subject   string
		committed time.Time
		want      bool
	}{
		{"[azx:8817364a] Init: scaffold camp workspace", after, true},
		{"[obey-campaign:8deed8b4-FE-CC0008] Init: scaffold camp workspace", before, true},
		{"[demo:1a2b3c4d] Repair: camp repair", after, true},
		{"[ccgoplanning:78887e36] Repair: campaign repair", after, true},
		{"[OBEY-CAMPAIGN-78887e36] Repair: campaign repair", selectiveRepairSince, true},
		{"[OBEY-CAMPAIGN-78887e36] Repair: campaign repair", before, false},
		{"[demo:1a2b3c4d] Repair: camp repair", time.Time{}, false},
		{"[ironman:4ba7f4bf] Evil jarvis testing", after, false},
		{"[OBEY-CAMPAIGN-6b55fceb] camp init repair", after, false},
		{"[OBEY-CAMPAIGN-78887e36] Update", after, false},
		{"[demo:1a2b3c4d] Repair: camp repair (by hand)", after, false},
		{"Init: scaffold camp workspace", after, false},
		{"[scope:msg] Init: scaffold camp workspace", after, false},
	}
	for _, c := range cases {
		if got := IsScaffoldCommit(c.subject, c.committed); got != c.want {
			t.Errorf("IsScaffoldCommit(%q, %s) = %v, want %v", c.subject, c.committed, got, c.want)
		}
	}
}
