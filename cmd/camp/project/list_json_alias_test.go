package project

import "testing"

func TestProjectListJSONAliasRegistered(t *testing.T) {
	if projectListCmd.Flags().Lookup("json") == nil {
		t.Fatal("camp project list missing --json alias")
	}
}
