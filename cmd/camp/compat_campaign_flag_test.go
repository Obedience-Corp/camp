package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// campaignFlagName is the frozen spelling of the target-camp flag. It is typed
// by users, baked into scripts, and completed by the installed shell
// integration; docs/terminology.md freezes the flag and changes only its
// description.
const campaignFlagName = "campaign"

// campaignFlagCarriers are the commands shipping --campaign today. It is a
// floor, not an exact set: a new command may add the flag, but none of these
// may drop it, because a script or alias already passes it to each one.
var campaignFlagCarriers = []string{
	"camp attach",
	"camp idea add",
	"camp jobs run",
	"camp project add",
	"camp project link",
	"camp project rename",
	"camp project unlink",
}

// TestCampaignFlagSpellingIsFrozen walks every registered command and asserts
// that each one offering the target-camp flag still spells it --campaign with a
// -c shorthand. A rename would not fail a build: cobra would simply reject the
// flag at runtime, in whatever script or alias already passes it.
func TestCampaignFlagSpellingIsFrozen(t *testing.T) {
	found := map[string]bool{}

	eachCommand(rootCmd, func(cmd *cobra.Command) {
		flag := cmd.Flags().Lookup(campaignFlagName)
		if flag == nil {
			return
		}
		path := cmd.CommandPath()
		found[path] = true

		if flag.Shorthand != "" && flag.Shorthand != "c" {
			t.Errorf("%s: --campaign shorthand is %q, want %q", path, flag.Shorthand, "c")
		}
		if flag.Value.Type() != "string" {
			t.Errorf("%s: --campaign is a %s, want a string", path, flag.Value.Type())
		}
	})

	for _, want := range campaignFlagCarriers {
		if !found[want] {
			t.Errorf("%q no longer offers --campaign", want)
		}
	}

	paths := make([]string, 0, len(found))
	for path := range found {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	t.Logf("--campaign is carried by %d commands: %s", len(paths), strings.Join(paths, ", "))
}

// TestCampaignFlagInteractiveDefaultSurvives pins the no-value form. Passing
// --campaign with no argument opens the picker, which only works while the flag
// carries a NoOptDefVal sentinel.
func TestCampaignFlagInteractiveDefaultSurvives(t *testing.T) {
	for _, path := range [][]string{
		{"idea", "add"},
		{"attach"},
		{"project", "add"},
	} {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			cmd, _, err := rootCmd.Find(path)
			if err != nil {
				t.Fatalf("finding %v: %v", path, err)
			}
			flag := cmd.Flags().Lookup(campaignFlagName)
			if flag == nil {
				t.Fatalf("%s no longer registers --campaign", cmd.CommandPath())
			}
			if flag.NoOptDefVal == "" {
				t.Fatalf("%s: --campaign lost its no-value interactive default", cmd.CommandPath())
			}
			if flag.Shorthand != "c" {
				t.Fatalf("%s: --campaign shorthand is %q, want %q", cmd.CommandPath(), flag.Shorthand, "c")
			}
		})
	}
}

// TestCampaignFlagHelpCarriesNoDeprecationNotice guards the copy rule that has
// teeth: warning about a frozen contract tells users to change something that
// must not change.
func TestCampaignFlagHelpCarriesNoDeprecationNotice(t *testing.T) {
	banned := []string{"deprecated", "legacy", "old name", "will be renamed", "backwards compatibility"}

	eachCommand(rootCmd, func(cmd *cobra.Command) {
		flag := cmd.Flags().Lookup(campaignFlagName)
		if flag == nil {
			return
		}
		if flag.Deprecated != "" {
			t.Errorf("%s: --campaign is marked deprecated; it is a supported spelling, not legacy debt", cmd.CommandPath())
		}
		usage := strings.ToLower(flag.Usage)
		for _, word := range banned {
			if strings.Contains(usage, word) {
				t.Errorf("%s: --campaign help says %q: %s", cmd.CommandPath(), word, flag.Usage)
			}
		}
	})
}

func eachCommand(cmd *cobra.Command, visit func(*cobra.Command)) {
	visit(cmd)
	for _, child := range cmd.Commands() {
		eachCommand(child, visit)
	}
}
