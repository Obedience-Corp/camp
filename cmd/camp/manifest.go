package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// Manifest represents the CLI command restriction manifest.
type Manifest struct {
	Version  int            `json:"version"`
	CLI      string         `json:"cli"`
	Commands []CommandEntry `json:"commands"`
}

// CommandEntry represents a single command's agent restriction metadata.
type CommandEntry struct {
	Path         string `json:"path"`
	AgentAllowed bool   `json:"agent_allowed"`
	Reason       string `json:"reason,omitempty"`
	Interactive  bool   `json:"interactive,omitempty"`
}

var manifestCmd = &cobra.Command{
	Use:    "__manifest",
	Short:  "Output command manifest for daemon enforcement",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manifest := Manifest{
			Version:  1,
			CLI:      "camp",
			Commands: []CommandEntry{},
		}
		walkCommands(cmd.Root(), "", &manifest.Commands)
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return err
	},
}

// walkCommands recursively traverses the command tree and collects
// commands that have agent restriction annotations.
func walkCommands(cmd *cobra.Command, prefix string, entries *[]CommandEntry) {
	for _, child := range cmd.Commands() {
		path := child.Name()
		if prefix != "" {
			path = prefix + " " + child.Name()
		}

		// Only include commands that have the agent_allowed annotation
		if val, ok := child.Annotations["agent_allowed"]; ok {
			entry := CommandEntry{
				Path:         path,
				AgentAllowed: val == "true",
			}
			if reason, ok := child.Annotations["agent_reason"]; ok {
				entry.Reason = reason
			}
			if interactive, ok := child.Annotations["interactive"]; ok {
				entry.Interactive = interactive == "true"
			}
			*entries = append(*entries, entry)
		}

		// Always recurse into subcommands regardless of parent annotation
		walkCommands(child, path, entries)
	}
}

func init() {
	rootCmd.AddCommand(manifestCmd)
}
