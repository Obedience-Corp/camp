package main

import (
	"os"
	"reflect"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
)

func TestExpandShortcutsMultiWordConcept(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(campaign.EnvCacheDisable, "1")

	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	os.Args = []string{"camp", "wt", "list"}
	expandShortcuts()

	want := []string{"camp", "project", "worktree", "list"}
	if !reflect.DeepEqual(os.Args, want) {
		t.Fatalf("os.Args = %#v, want %#v", os.Args, want)
	}
}
