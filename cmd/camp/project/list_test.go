package project

import "testing"

func TestProjectListCountFlagRegistered(t *testing.T) {
	if projectListCmd.Flags().Lookup("count") == nil {
		t.Fatal("camp project list missing --count flag")
	}
}
