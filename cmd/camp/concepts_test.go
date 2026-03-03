package main

import "testing"

func TestConceptsCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "concepts" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("concepts command not registered on root")
	}
}

func TestConceptsGroupID(t *testing.T) {
	if conceptsCmd.GroupID != "campaign" {
		t.Errorf("GroupID = %q, want %q", conceptsCmd.GroupID, "campaign")
	}
}

func TestConceptsAlias(t *testing.T) {
	aliases := conceptsCmd.Aliases
	found := false
	for _, a := range aliases {
		if a == "concept" {
			found = true
			break
		}
	}
	if !found {
		t.Error("concepts command missing 'concept' alias")
	}
}
