package skills

import (
	"fmt"

	"github.com/Obedience-Corp/camp/internal/campaign"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
	"github.com/spf13/cobra"
)

var skillsLinkCmd = &cobra.Command{
	Use:   "link",
	Short: "Project campaign skill bundles into tool-specific skills directories",
	Long: `Project campaign skill bundles from .campaign/skills/ into tool-specific
skills directories.

This command creates one symlink per skill bundle. It does not replace entire
provider skills directories, so existing user skills remain intact.

Examples:
  camp skills link --tool claude       Project skills into .claude/skills/
  camp skills link --tool agents       Project skills into .agents/skills/
  camp skills link --path custom/dir   Project skills into custom/dir
  camp skills link --tool claude -n    Dry run — show what would happen
  camp skills link --tool claude -f    Replace conflicting symlink entries`,
	Args: cobra.NoArgs,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Agents use --tool or --path flags directly",
	},
	RunE: runSkillsLink,
}

func init() {
	Cmd.AddCommand(skillsLinkCmd)

	flags := skillsLinkCmd.Flags()
	flags.StringP("tool", "t", "", "Tool to link: claude, agents")
	flags.StringP("path", "p", "", "Custom destination directory")
	flags.BoolP("force", "f", false, "Replace conflicting symlink entries (never files/directories)")
	flags.BoolP("dry-run", "n", false, "Show what would happen without making changes")
}

func runSkillsLink(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	tool, _ := cmd.Flags().GetString("tool")
	destPath, _ := cmd.Flags().GetString("path")
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if tool != "" && destPath != "" {
		return fmt.Errorf("--tool and --path are mutually exclusive; use one or the other")
	}
	if tool == "" && destPath == "" {
		return fmt.Errorf("specify --tool <name> or --path <destination>\n\nAvailable tools: claude, agents")
	}

	skillsDir, err := intskills.FindSkillsDir(ctx)
	if err != nil {
		return err
	}

	root, err := campaign.DetectCached(ctx)
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

	if err := intskills.EnsureProjectionDirectory(dest, dryRun, errOut); err != nil {
		return err
	}

	summary, err := intskills.ProjectSkillEntries(dest, skillsDir, slugs, dryRun, force)
	if err != nil {
		return err
	}

	verb := "projected"
	if dryRun {
		verb = "would project"
	}
	if _, err := fmt.Fprintf(out, "%s %d skill bundle(s) into %s (created=%d replaced=%d unchanged=%d)\n",
		verb, summary.Created+summary.Replaced+summary.AlreadyLinked, dest, summary.Created, summary.Replaced, summary.AlreadyLinked); err != nil {
		return err
	}

	if summary.Conflicts > 0 {
		return fmt.Errorf(
			"projection incomplete: %d conflicting skill path(s) exist and were not overwritten: %v",
			summary.Conflicts,
			summary.ConflictNames,
		)
	}

	if summary.Created == 0 && summary.Replaced == 0 && summary.AlreadyLinked == len(slugs) {
		if _, err := fmt.Fprintf(out, "already linked: all campaign skill bundles are projected into %s\n", dest); err != nil {
			return err
		}
	}

	return nil
}
