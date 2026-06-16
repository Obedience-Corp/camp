package main

import "testing"

func TestListJSONAliasRegistered(t *testing.T) {
	if listCmd.Flags().Lookup("json") == nil {
		t.Fatal("camp list missing --json alias")
	}
}
