package git

import "testing"

func TestParseGitStatusPorcelainZLeadingSpace(t *testing.T) {
	entries := ParseStatusPorcelainZ([]byte(" M file.go\x00"))
	if len(entries) != 1 || entries[0].Code != " M" || entries[0].Path != "file.go" {
		t.Fatalf("got %+v", entries)
	}
}
