package intent

import (
	"path/filepath"
	"testing"
)

func TestDisplayPath_RelativeUnderCampaign(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, ".campaign", "intents", "notes", "x.md")
	got := displayPath(root, abs)
	want := ".campaign/intents/notes/x.md"
	if got != want {
		t.Fatalf("displayPath = %q, want %q", got, want)
	}
}

func TestDisplayPath_AlreadyRelative(t *testing.T) {
	root := t.TempDir()
	got := displayPath(root, ".campaign/intents/notes/y.md")
	if got != ".campaign/intents/notes/y.md" {
		t.Fatalf("displayPath relative input = %q", got)
	}
}

func TestDisplayPath_OutsideCampaignKeepsAbsolute(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "other.md")
	got := displayPath(root, outside)
	if got != outside {
		t.Fatalf("outside path should stay absolute, got %q want %q", got, outside)
	}
}

func TestDisplayPath_EmptyRootPassthrough(t *testing.T) {
	p := "/tmp/x.md"
	if got := displayPath("", p); got != p {
		t.Fatalf("empty root: got %q", got)
	}
}
