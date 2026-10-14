package starter

import (
	"path/filepath"
	"testing"
)

func TestValidateStateRejectsIncompleteOrUnownedPaths(t *testing.T) {
	root, err := filepath.Abs("camps")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, Name)
	staging := filepath.Join(root, ".festival-setup-123", Name)
	for _, s := range []state{
		{}, {Version: 2}, {Version: 1, Path: "relative/festival"},
		{Version: 1, Path: target, Staging: filepath.Join(root, "other", Name)},
		{Version: 1, Path: target, Ready: true},
	} {
		if err := validateState(s); err == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
	for _, s := range []state{
		{Version: 1}, {Version: 1, Completed: true},
		{Version: 1, Path: target, Staging: staging, Ready: true},
	} {
		if err := validateState(s); err != nil {
			t.Fatalf("rejected %+v: %v", s, err)
		}
	}
}
