package commit

import "testing"

func TestProjectAction_Values_LinkedActions(t *testing.T) {
	tests := []struct {
		action ProjectAction
		want   string
	}{
		{ProjectLink, "Link"},
		{ProjectUnlink, "Unlink"},
	}

	for _, tt := range tests {
		if got := string(tt.action); got != tt.want {
			t.Fatalf("%q = %q, want %q", tt.action, got, tt.want)
		}
	}
}
