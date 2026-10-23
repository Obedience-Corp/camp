package commit

import "testing"

func TestIsScaffoldCommit(t *testing.T) {
	cases := []struct {
		subject string
		want    bool
	}{
		{"[azx:8817364a] Init: scaffold camp workspace", true},
		{"[obey-campaign:8deed8b4-FE-CC0008] Init: scaffold camp workspace", true},
		{"[demo:1a2b3c4d] Repair: camp repair", true},
		{"[ccgoplanning:78887e36] Repair: campaign repair", true},
		{"[OBEY-CAMPAIGN-78887e36] Repair: campaign repair", true},
		{"[ironman:4ba7f4bf] Evil jarvis testing", false},
		{"[OBEY-CAMPAIGN-78887e36] Update", false},
		{"[demo:1a2b3c4d] Repair: camp repair (by hand)", false},
		{"Init: scaffold camp workspace", false},
		{"[scope:msg] Init: scaffold camp workspace", false},
	}
	for _, c := range cases {
		if got := IsScaffoldCommit(c.subject); got != c.want {
			t.Errorf("IsScaffoldCommit(%q) = %v, want %v", c.subject, got, c.want)
		}
	}
}
