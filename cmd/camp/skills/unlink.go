package skills

import (
	"fmt"

	"github.com/Obedience-Corp/camp/internal/campaign"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
	"github.com/spf13/cobra"
)

var skillsUnlinkCmd = &cobra.Command{
	Use:   "unlink",
	Short: "Remove projected skill bundle symlinks",
	Long: `Remove managed skill bundle symlinks created by 'camp skills link'.

Only removes projected symlink entries created from .campaign/skills bundles.
It never removes non-symlink files/directories or foreign symlinks.

Examples:
  camp skills unlink --tool claude       Remove projected entries in .claude/skills/
  camp skills unlink --tool agents       Remove projected entries in .agents/skills/
  camp skills unlink --path custom/dir   Remove projected entries in custom/dir
  camp skills unlink --tool claude -n    Dry run — show what would happen`,
	Args: cobra.NoArgs,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Agents use --tool or --path flags directly",
	},
	RunE: runSkillsUnlink,
}

func init() {
	Cmd.AddCommand(skillsUnlinkCmd)

	flags := skillsUnlinkCmd.Flags()
	flags.StringP("tool", "t", "", "Tool to unlink: claude, agents")
	flags.StringP("path", "p", "", "Custom destination directory to unlink")
	flags.BoolP("dry-run", "n", false, "Show what would happen without making changes")
}

func runSkillsUnlink(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	tool, _ := cmd.Flags().GetString("tool")
	destPath, _ := cmd.Flags().GetString("path")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if tool != "" && destPath != "" {
		return fmt.Errorf("--tool and --path are mutually exclusive; use one or the other")
	}
	if tool == "" && destPath == "" {
		return fmt.Errorf("specify --tool <name> or --path <destination>\n\nAvailable tools: claude, agents")
	}

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	skillsDir, err := intskills.FindSkillsDir(ctx)
	if err != nil {
		return err
	}

	dest, err := resolveSkillsDestination(root, tool, destPath)
	if err != nil {
		return err
	}

	if err := intskills.ValidateDestination(dest, root); err != nil {
		return err
	}

	slugs, err := intskills.DiscoverSkillSlugs(skillsDir)
	if err != nil {
		return err
	}
	if len(slugs) == 0 {
		if _, err := fmt.Fprintf(out, "no skill bundles found in %s\n", skillsDir); err != nil {
			return err
		}
		return nil
	}

	pathType, err := intskills.CheckPathType(dest)
	if err != nil {
		return err
	}

	switch pathType {
	case intskills.TypeMissing:
		if _, err := fmt.Fprintf(out, "not linked: %s\n", dest); err != nil {
			return err
		}
		return nil
	case intskills.TypeFile, intskills.TypeSymlink:
		return fmt.Errorf("destination is not a projection directory: %s", dest)
	}

	removed, err := intskills.RemoveProjectedSkillEntries(dest, skillsDir, slugs, dryRun)
	if err != nil {
		return err
	}
	if dryRun {
		if _, err := fmt.Fprintf(out, "would remove %d projected skill symlink(s) from %s\n", removed, dest); err != nil {
			return err
		}
		return nil
	}
	if removed == 0 {
		if _, err := fmt.Fprintf(out, "not linked: no projected skill symlinks found in %s\n", dest); err != nil {
			return err
		}
		return nil
	}
	if _, err := fmt.Fprintf(out, "unlinked %d skill bundle(s) from %s\n", removed, dest); err != nil {
		return err
	}
	return nil
}
