package intent

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// findIntentSubcommand returns the named subcommand under intent.Cmd.
func findIntentSubcommand(name string) *cobra.Command {
	for _, c := range Cmd.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

func TestIntentCrawl_RegisteredUnderIntent(t *testing.T) {
	cmd := findIntentSubcommand("crawl")
	if cmd == nil {
		t.Fatal("camp intent crawl is not registered under camp intent")
	}
}

func TestIntentCrawl_HasInteractiveAgentAnnotations(t *testing.T) {
	cmd := findIntentSubcommand("crawl")
	if cmd == nil {
		t.Fatal("camp intent crawl missing")
	}
	if got := cmd.Annotations["agent_allowed"]; got != "false" {
		t.Errorf("agent_allowed = %q, want %q", got, "false")
	}
	if got := cmd.Annotations["interactive"]; got != "true" {
		t.Errorf("interactive = %q, want %q", got, "true")
	}
	if got := cmd.Annotations["agent_reason"]; got == "" {
		t.Error("agent_reason annotation is empty")
	}
}

func TestIntentCrawl_DeclaresExpectedFlags(t *testing.T) {
	cmd := findIntentSubcommand("crawl")
	if cmd == nil {
		t.Fatal("camp intent crawl missing")
	}
	for _, name := range []string{"status", "limit", "sort", "no-commit"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
}

func TestIntentCrawl_LongDescriptionMentionsLiveScope(t *testing.T) {
	cmd := findIntentSubcommand("crawl")
	if cmd == nil {
		t.Fatal("camp intent crawl missing")
	}
	for _, want := range []string{"inbox", "ready", "active"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("Long help missing %q", want)
		}
	}
}

func TestParseStatusFlags_DedupesRepeats(t *testing.T) {
	got, err := parseStatusFlags([]string{"inbox", "INBOX", "ready"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 unique statuses, got %v", got)
	}
}

func TestParseStatusFlags_PassesThroughErrors(t *testing.T) {
	if _, err := parseStatusFlags([]string{"dungeon/done"}); err == nil {
		t.Fatal("expected error for dungeon status")
	}
}
