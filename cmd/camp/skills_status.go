package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/skills"
)

var skillsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current state of skill symlinks",
	Long: `Show the current state of skill symlinks for all known tool targets.

Reports whether each tool's skills directory is properly linked to
.campaign/skills/, broken, missing, or blocked by an existing path.

Examples:
  camp skills status          Show link states in table format
  camp skills status --json   Machine-readable JSON output`,
	Args: cobra.NoArgs,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive status listing",
	},
	RunE: runSkillsStatus,
}

func init() {
	skillsCmd.AddCommand(skillsStatusCmd)
	skillsStatusCmd.Flags().Bool("json", false, "Output as JSON")
}

type skillStatusEntry struct {
	Tool       string `json:"tool"`
	Path       string `json:"path"`
	State      string `json:"state"`
	Target     string `json:"target,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

func runSkillsStatus(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	jsonOutput, _ := cmd.Flags().GetBool("json")

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	skillsDir := filepath.Join(root, campaign.CampaignDir, skills.SkillsSubdir)
	if _, err := os.Stat(skillsDir); err != nil {
		return fmt.Errorf(".campaign/skills/ not found: run 'camp init' or create the directory")
	}

	// Collect status for each tool
	var entries []skillStatusEntry
	hasBroken := false

	// Sort tool names for consistent output
	toolNames := make([]string, 0, len(skills.ToolPaths))
	for name := range skills.ToolPaths {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)

	for _, tool := range toolNames {
		relPath := skills.ToolPaths[tool]
		destPath := filepath.Join(root, relPath)

		state, err := skills.CheckLinkState(destPath, skillsDir)
		if err != nil {
			return fmt.Errorf("check %s: %w", tool, err)
		}

		entry := skillStatusEntry{
			Tool: tool,
			Path: relPath,
		}

		switch state {
		case skills.StateValid:
			// Read the raw symlink target for display
			rawTarget, _ := os.Readlink(destPath)
			entry.State = "linked"
			entry.Target = rawTarget

		case skills.StateBroken:
			entry.State = "broken"
			entry.Suggestion = fmt.Sprintf("run 'camp skills link --tool %s --force' to fix", tool)
			hasBroken = true

		case skills.StateNotALink:
			entry.State = "blocked"
			entry.Suggestion = fmt.Sprintf("non-symlink exists at %s; use --force to replace", relPath)
			hasBroken = true

		case skills.StateMissing:
			entry.State = "not linked"
			entry.Suggestion = fmt.Sprintf("run 'camp skills link --tool %s' to create", tool)
		}

		entries = append(entries, entry)
	}

	if jsonOutput {
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal JSON: %w", err)
		}
		fmt.Fprintln(out, string(data))
	} else {
		// Table output
		fmt.Fprintf(out, "%-10s %-20s %s\n", "Tool", "Path", "Status")
		fmt.Fprintf(out, "%-10s %-20s %s\n", "----", "----", "------")

		for _, e := range entries {
			status := e.State
			if e.Target != "" {
				status = fmt.Sprintf("%s -> %s", status, e.Target)
			}
			fmt.Fprintf(out, "%-10s %-20s %s\n", e.Tool, e.Path, status)
		}

		// Print suggestions
		for _, e := range entries {
			if e.Suggestion != "" {
				fmt.Fprintf(out, "\n  %s: %s", e.Tool, e.Suggestion)
			}
		}
		// Trailing newline if suggestions were printed
		for _, e := range entries {
			if e.Suggestion != "" {
				fmt.Fprintln(out)
				break
			}
		}
	}

	if hasBroken {
		return fmt.Errorf("one or more skill links need attention")
	}

	return nil
}
