package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
	"github.com/spf13/cobra"
)

const SkillsStatusJSONVersion = "skills-status/v1alpha1"

var skillsStatusJSON bool

var skillsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current state of projected skill bundle symlinks",
	Long: `Show projection status for campaign skill bundles across tool targets.

Reports whether each tool's skills directory has projected entries from
.campaign/skills/, is partially projected, missing, broken, or blocked.

Examples:
  camp skills status          Show projection states in table format
  camp skills status --json   Machine-readable JSON output`,
	Args: cobra.NoArgs,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive status listing",
	},
	RunE: jsoncontract.RunE(SkillsStatusJSONVersion, func() bool { return skillsStatusJSON }, runSkillsStatus),
}

func init() {
	Cmd.AddCommand(skillsStatusCmd)
	skillsStatusCmd.Flags().BoolVar(&skillsStatusJSON, "json", false, "Output as JSON")
	skillsStatusCmd.Flags().Bool("strict", false, "Return non-zero exit code when links need attention (for CI)")
	skillsStatusCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(SkillsStatusJSONVersion, func() bool { return skillsStatusJSON }))
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

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	skillsDir := filepath.Join(root, campaign.CampaignDir, intskills.SkillsSubdir)
	if _, err := os.Stat(skillsDir); err != nil {
		return camperrors.Newf(".campaign/skills/ not found: run 'camp init' or create the directory")
	}

	slugs, err := intskills.DiscoverSkillSlugs(skillsDir)
	if err != nil {
		return err
	}
	if len(slugs) == 0 {
		if skillsStatusJSON {
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
	toolPaths := intskills.ToolPaths()

	for _, tool := range toolNames {
		relPath := toolPaths[tool]
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

	// Project status for each project worktree (.agents/skills primary).
	wtEntries, wtAttention, err := worktreeSkillStatusEntries(root, skillsDir, slugs)
	if err != nil {
		return err
	}
	entries = append(entries, wtEntries...)
	if wtAttention {
		hasAttention = true
	}

	if skillsStatusJSON {
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return camperrors.Wrap(err, "marshal JSON")
		}
		if _, err := fmt.Fprintln(out, string(data)); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(out, "%-10s %-52s %s\n", "Tool", "Path", "Status"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "%-10s %-52s %s\n", "----", "----", "------"); err != nil {
			return err
		}

		for _, e := range entries {
			status := e.State
			if e.Target != "" {
				status = fmt.Sprintf("%s -> %s", status, e.Target)
			}
			if _, err := fmt.Fprintf(out, "%-10s %-52s %s\n", e.Tool, e.Path, status); err != nil {
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
		return camperrors.Newf("one or more skill projection targets need attention")
	}

	return nil
}

// worktreeSkillStatusEntries builds status rows for each project worktree.
func worktreeSkillStatusEntries(root, skillsDir string, slugs []string) ([]skillStatusEntry, bool, error) {
	cfg, err := config.LoadCampaignConfig(context.Background(), root)
	if err != nil {
		cfg = &config.CampaignConfig{}
	}
	resolver := paths.NewResolver(root, cfg.Paths())
	wtRoot := resolver.Worktrees()
	roots, err := intskills.ListWorktreeRoots(wtRoot)
	if err != nil {
		return nil, false, err
	}

	var entries []skillStatusEntry
	hasAttention := false
	for _, wtPath := range roots {
		rel, err := filepath.Rel(root, wtPath)
		if err != nil {
			rel = wtPath
		}
		entry := skillStatusEntry{
			Tool: "worktree",
			Path: filepath.ToSlash(rel),
		}
		projState, err := intskills.InspectWorktreeProjection(wtPath, skillsDir, slugs)
		if err != nil {
			return nil, false, camperrors.Wrapf(err, "inspect worktree %s", rel)
		}
		switch {
		case projState.Conflicts > 0:
			entry.State = fmt.Sprintf("blocked (%d conflict)", projState.Conflicts)
			entry.Suggestion = fmt.Sprintf("resolve conflicting entries in %s/.agents/skills then rerun link --worktrees-only", rel)
			hasAttention = true
		case projState.Broken > 0:
			entry.State = fmt.Sprintf("broken (%d)", projState.Broken)
			entry.Suggestion = "run 'camp skills link --worktrees-only --force' to repair worktree skill links"
			hasAttention = true
		case projState.Mismatched > 0:
			entry.State = fmt.Sprintf("mismatched (%d)", projState.Mismatched)
			entry.Suggestion = "run 'camp skills link --worktrees-only --force' to update worktree skill links"
			hasAttention = true
		case projState.Linked == 0:
			entry.State = "not linked"
			entry.Suggestion = "run 'camp skills link --worktrees-only' to project skills into worktrees"
		case projState.Linked < projState.TotalSkills:
			entry.State = fmt.Sprintf("partial (%d/%d)", projState.Linked, projState.TotalSkills)
			entry.Suggestion = "run 'camp skills link --worktrees-only' to sync missing worktree skill links"
		default:
			entry.State = fmt.Sprintf("linked (%d/%d)", projState.Linked, projState.TotalSkills)
		}
		entries = append(entries, entry)
	}
	return entries, hasAttention, nil
}
