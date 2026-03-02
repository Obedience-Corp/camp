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
	Short: "Show the current state of projected skill bundle symlinks",
	Long: `Show projection status for campaign skill bundles across tool targets.

Reports whether each tool's skills directory has projected entries from
.campaign/skills/, is partially projected, missing, broken, or blocked.

Examples:
  camp skills status          Show projection states in table format
  camp skills status --json   Machine-readable JSON output`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
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

	slugs, err := skills.DiscoverSkillSlugs(skillsDir)
	if err != nil {
		return err
	}
	if len(slugs) == 0 {
		if jsonOutput {
			fmt.Fprintln(out, "[]")
		} else {
			fmt.Fprintln(out, "No skill bundles found in .campaign/skills/")
		}
		return nil
	}

	// Collect status for each tool
	var entries []skillStatusEntry
	hasAttention := false

	// Sort tool names for consistent output
	toolNames := make([]string, 0, len(skills.ToolPaths))
	for name := range skills.ToolPaths {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)

	for _, tool := range toolNames {
		relPath := skills.ToolPaths[tool]
		destPath := filepath.Join(root, relPath)

		pathType, err := skills.CheckPathType(destPath)
		if err != nil {
			return fmt.Errorf("check %s: %w", tool, err)
		}

		entry := skillStatusEntry{
			Tool: tool,
			Path: relPath,
		}

		switch pathType {
		case skills.TypeMissing:
			entry.State = "not linked"
			entry.Suggestion = fmt.Sprintf("run 'camp skills link --tool %s' to project skill bundles", tool)

		case skills.TypeFile, skills.TypeSymlink:
			entry.State = "blocked"
			entry.Suggestion = fmt.Sprintf("path exists but is not a directory: %s", relPath)
			hasAttention = true

		case skills.TypeDirectory:
			projState, err := inspectSkillProjection(destPath, skillsDir, slugs)
			if err != nil {
				return fmt.Errorf("inspect %s projection: %w", tool, err)
			}

			switch {
			case projState.Conflicts > 0:
				entry.State = fmt.Sprintf("blocked (%d conflict)", projState.Conflicts)
				entry.Suggestion = fmt.Sprintf("resolve conflicting entries in %s then rerun link", relPath)
				hasAttention = true
			case projState.Broken > 0:
				entry.State = fmt.Sprintf("broken (%d)", projState.Broken)
				entry.Suggestion = fmt.Sprintf("run 'camp skills link --tool %s --force' to repair broken symlink entries", tool)
				hasAttention = true
			case projState.Linked == 0:
				entry.State = "not linked"
				entry.Suggestion = fmt.Sprintf("run 'camp skills link --tool %s' to project skill bundles", tool)
			case projState.Linked < projState.TotalSkills:
				entry.State = fmt.Sprintf("partial (%d/%d)", projState.Linked, projState.TotalSkills)
				entry.Suggestion = fmt.Sprintf("run 'camp skills link --tool %s' to sync missing skill bundle links", tool)
			default:
				entry.State = fmt.Sprintf("linked (%d/%d)", projState.Linked, projState.TotalSkills)
			}
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

	if hasAttention {
		return fmt.Errorf("one or more skill projection targets need attention")
	}

	return nil
}
