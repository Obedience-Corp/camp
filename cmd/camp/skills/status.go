package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
	"github.com/spf13/cobra"
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
	Cmd.AddCommand(skillsStatusCmd)
	skillsStatusCmd.Flags().Bool("json", false, "Output as JSON")
	skillsStatusCmd.Flags().Bool("strict", false, "Return non-zero exit code when links need attention (for CI)")
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

	skillsDir := filepath.Join(root, campaign.CampaignDir, intskills.SkillsSubdir)
	if _, err := os.Stat(skillsDir); err != nil {
		return fmt.Errorf(".campaign/skills/ not found: run 'camp init' or create the directory")
	}

	slugs, err := intskills.DiscoverSkillSlugs(skillsDir)
	if err != nil {
		return err
	}
	if len(slugs) == 0 {
		if jsonOutput {
			if _, err := fmt.Fprintln(out, "[]"); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintln(out, "No skill bundles found in .campaign/skills/"); err != nil {
				return err
			}
		}
		return nil
	}

	var entries []skillStatusEntry
	hasAttention := false

	toolNames := intskills.ToolNames()
	paths := intskills.ToolPaths()

	for _, tool := range toolNames {
		relPath := paths[tool]
		destPath := filepath.Join(root, relPath)

		pathType, err := intskills.CheckPathType(destPath)
		if err != nil {
			return camperrors.Wrapf(err, "check %s", tool)
		}

		entry := skillStatusEntry{
			Tool: tool,
			Path: relPath,
		}

		switch pathType {
		case intskills.TypeMissing:
			entry.State = "not linked"
			entry.Suggestion = fmt.Sprintf("run 'camp skills link --tool %s' to project skill bundles", tool)

		case intskills.TypeFile, intskills.TypeSymlink:
			entry.State = "blocked"
			entry.Suggestion = fmt.Sprintf("path exists but is not a directory: %s", relPath)
			hasAttention = true

		case intskills.TypeDirectory:
			projState, err := intskills.InspectSkillProjection(destPath, skillsDir, slugs)
			if err != nil {
				return camperrors.Wrapf(err, "inspect %s projection", tool)
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
			case projState.Mismatched > 0:
				entry.State = fmt.Sprintf("mismatched (%d)", projState.Mismatched)
				entry.Suggestion = fmt.Sprintf("run 'camp skills link --tool %s --force' to update mismatched symlink entries", tool)
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
			return camperrors.Wrap(err, "marshal JSON")
		}
		if _, err := fmt.Fprintln(out, string(data)); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(out, "%-10s %-20s %s\n", "Tool", "Path", "Status"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "%-10s %-20s %s\n", "----", "----", "------"); err != nil {
			return err
		}

		for _, e := range entries {
			status := e.State
			if e.Target != "" {
				status = fmt.Sprintf("%s -> %s", status, e.Target)
			}
			if _, err := fmt.Fprintf(out, "%-10s %-20s %s\n", e.Tool, e.Path, status); err != nil {
				return err
			}
		}

		for _, e := range entries {
			if e.Suggestion != "" {
				if _, err := fmt.Fprintf(out, "\n  %s: %s", e.Tool, e.Suggestion); err != nil {
					return err
				}
			}
		}
		for _, e := range entries {
			if e.Suggestion != "" {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
				break
			}
		}
	}

	strict, _ := cmd.Flags().GetBool("strict")
	if hasAttention && strict {
		return fmt.Errorf("one or more skill projection targets need attention")
	}

	return nil
}
