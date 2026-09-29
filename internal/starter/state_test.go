package starter

import "testing"

func TestValidateStateRejectsIncompleteOrUnownedPaths(t *testing.T) {
	for _, s := range []state{
		{}, {Version: 2}, {Version: 1, Path: "relative/festival"},
		{Version: 1, Path: "/camps/festival", Staging: "/other/festival"},
		{Version: 1, Path: "/camps/festival", Ready: true},
	} {
		if err := validateState(s); err == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
	for _, s := range []state{
		{Version: 1}, {Version: 1, Completed: true},
		{Version: 1, Path: "/camps/festival", Staging: "/camps/.festival-setup-123/festival", Ready: true},
	} {
		if err := validateState(s); err != nil {
			t.Fatalf("rejected %+v: %v", s, err)
		}
	}
}
