package crawl

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/crawl"
	"github.com/Obedience-Corp/camp/internal/intent"
)

func targetsOf(opts []crawl.Option) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.Target)
	}
	return out
}

func containsTarget(opts []crawl.Option, target string) bool {
	for _, o := range opts {
		if o.Target == target {
			return true
		}
	}
	return false
}

func TestFirstStepOptions_KeepLabelEmbedsStatus(t *testing.T) {
	opts := firstStepOptions(&intent.Intent{Status: intent.StatusReady})
	if opts[0].Label != "Keep in ready" {
		t.Errorf("keep label = %q, want %q", opts[0].Label, "Keep in ready")
	}
	if opts[len(opts)-1].Action != crawl.ActionQuit {
		t.Errorf("expected last action to be quit")
	}
}

func TestDestinationOptions_OmitsCurrentStatus(t *testing.T) {
	in := &intent.Intent{Status: intent.StatusReady}
	opts := destinationOptions(in, nil)
	for _, o := range opts {
		if o.Target == "ready" {
			t.Errorf("current status included in destinations: %v", targetsOf(opts))
		}
	}
}

func TestDestinationOptions_IncludesAllOthers(t *testing.T) {
	in := &intent.Intent{Status: intent.StatusInbox}
	opts := destinationOptions(in, nil)
	for _, want := range []string{"ready", "active", "dungeon/done", "dungeon/killed", "dungeon/archived", "dungeon/someday"} {
		if !containsTarget(opts, want) {
			t.Errorf("missing target %q (got %v)", want, targetsOf(opts))
		}
	}
}

func TestDestinationOptions_DungeonRequiresReason(t *testing.T) {
	in := &intent.Intent{Status: intent.StatusReady}
	opts := destinationOptions(in, nil)
	for _, o := range opts {
		want := intent.Status(o.Target).InDungeon()
		if o.RequiresReason != want {
			t.Errorf("target %q RequiresReason = %v, want %v", o.Target, o.RequiresReason, want)
		}
	}
}

func TestDestinationOptions_PromotedToBlocksLiveBackEntry(t *testing.T) {
	in := &intent.Intent{Status: intent.StatusActive, PromotedTo: "festivals/active/foo"}
	opts := destinationOptions(in, nil)
	for _, o := range opts {
		if o.Target == "inbox" || o.Target == "ready" {
			t.Errorf("promoted_to intent should not allow target %q", o.Target)
		}
	}
}

func TestDestinationOptions_IncludesCounts(t *testing.T) {
	in := &intent.Intent{Status: intent.StatusInbox}
	counts := map[intent.Status]int{
		intent.StatusReady:    5,
		intent.StatusArchived: 2,
	}
	opts := destinationOptions(in, counts)
	for _, o := range opts {
		switch o.Target {
		case "ready":
			if o.Count != 5 {
				t.Errorf("ready count = %d, want 5", o.Count)
			}
		case "dungeon/archived":
			if o.Count != 2 {
				t.Errorf("archived count = %d, want 2", o.Count)
			}
		}
	}
}

func TestCountsByStatus(t *testing.T) {
	in := []intent.StatusCount{
		{Status: intent.StatusInbox, Count: 3},
		{Status: intent.StatusActive, Count: 1},
	}
	got := countsByStatus(in)
	if got[intent.StatusInbox] != 3 || got[intent.StatusActive] != 1 {
		t.Errorf("countsByStatus = %v", got)
	}
}
