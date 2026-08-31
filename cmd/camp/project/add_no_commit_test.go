package project

import "testing"

func TestProjectAdd_NoCommitFlagRemoved(t *testing.T) {
	if projectAddCmd.Flags().Lookup("no-commit") != nil {
		t.Fatal("project add must always auto-commit; --no-commit must not be registered")
	}
}

func TestProjectNew_NoCommitFlagRemoved(t *testing.T) {
	if projectNewCmd.Flags().Lookup("no-commit") != nil {
		t.Fatal("project new must always auto-commit; --no-commit must not be registered")
	}
}
