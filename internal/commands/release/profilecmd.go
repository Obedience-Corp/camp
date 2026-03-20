package release

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// NewProfileCommand returns a hidden command that displays the release channel
// for every registered command. Build the dev binary to see the full picture.
func NewProfileCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:    "build-profile",
		Short:  "Show release channel for each command",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runProfile(root)
		},
	}
}

type profileEntry struct {
	name    string
	channel string
	group   string
}

func runProfile(root *cobra.Command) error {
	var entries []profileEntry

	for _, cmd := range root.Commands() {
		// Skip cobra built-ins and this command itself.
		if cmd.Name() == "help" || cmd.Name() == "completion" || cmd.Name() == "build-profile" {
			continue
		}

		// Skip hidden commands that have no release annotation.
		if cmd.Hidden {
			if _, ok := cmd.Annotations[AnnotationReleaseChannel]; !ok {
				continue
			}
		}

		channel := "stable"
		if ann := cmd.Annotations[AnnotationReleaseChannel]; ann == ReleaseChannelDevOnly {
			channel = "dev"
		}

		group := cmd.GroupID
		if group == "" {
			group = "-"
		}

		entries = append(entries, profileEntry{
			name:    cmd.Name(),
			channel: channel,
			group:   group,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "COMMAND\tCHANNEL\tGROUP\n")
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%s\n", e.name, e.channel, e.group)
	}
	return w.Flush()
}
