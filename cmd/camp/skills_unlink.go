package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/skills"
)

var skillsUnlinkCmd = &cobra.Command{
	Use:   "unlink",
	Short: "Remove skill symlinks",
	Long: `Remove managed skill symlinks created by 'camp skills link'.

Only removes symlinks that point to .campaign/skills/. Refuses to remove
non-symlink files, directories, or symlinks that point elsewhere.

Examples:
  camp skills unlink --tool claude       Remove .claude/skills symlink
  camp skills unlink --tool agents       Remove .agents/skills symlink
  camp skills unlink --path custom/dir   Remove custom symlink
  camp skills unlink --tool claude -n    Dry run — show what would happen`,
	Args: cobra.NoArgs,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Agents use --tool or --path flags directly",
	},
	RunE: runSkillsUnlink,
}

func init() {
	skillsCmd.AddCommand(skillsUnlinkCmd)

	flags := skillsUnlinkCmd.Flags()
	flags.StringP("tool", "t", "", "Tool to unlink: claude, agents")
	flags.StringP("path", "p", "", "Custom destination path to unlink")
	flags.BoolP("dry-run", "n", false, "Show what would happen without making changes")
}

func runSkillsUnlink(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	tool, _ := cmd.Flags().GetString("tool")
	destPath, _ := cmd.Flags().GetString("path")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Validate mutual exclusivity
	if tool != "" && destPath != "" {
		return fmt.Errorf("--tool and --path are mutually exclusive; use one or the other")
	}
	if tool == "" && destPath == "" {
		return fmt.Errorf("specify --tool <name> or --path <destination>\n\nAvailable tools: claude, agents")
	}

	// Get campaign root and skills dir
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	skillsDir, err := skills.FindSkillsDir(ctx)
	if err != nil {
		return err
	}

	// Resolve destination path
	var dest string
	if tool != "" {
		relPath, err := skills.ResolveToolPath(tool)
		if err != nil {
			return err
		}
		dest = filepath.Join(root, relPath)
	} else {
		if filepath.IsAbs(destPath) {
			dest = destPath
		} else {
			dest = filepath.Join(root, destPath)
		}
	}

	// Check current state
	state, err := skills.CheckLinkState(dest, skillsDir)
	if err != nil {
		return err
	}

	switch state {
	case skills.StateMissing:
		fmt.Fprintf(out, "not linked: %s\n", dest)
		return nil

	case skills.StateNotALink:
		return fmt.Errorf("path exists but is not a symlink — refusing to remove: %s", dest)

	case skills.StateBroken:
		// Broken symlink — check if it was a managed link by reading the raw target
		rawTarget, readErr := os.Readlink(dest)
		if readErr != nil {
			return fmt.Errorf("read symlink: %w", readErr)
		}
		// Resolve relative targets against the symlink's directory
		absTarget := rawTarget
		if !filepath.IsAbs(rawTarget) {
			absTarget = filepath.Join(filepath.Dir(dest), rawTarget)
		}
		// Check if this was supposed to point to our skills dir
		resolvedSkills, _ := filepath.EvalSymlinks(skillsDir)
		absTargetClean := filepath.Clean(absTarget)
		if absTargetClean != skillsDir && absTargetClean != resolvedSkills {
			return fmt.Errorf("symlink exists but is not managed by camp skills — refusing to remove: %s -> %s", dest, rawTarget)
		}
		// It was managed but target is gone — safe to remove

	case skills.StateValid:
		// Valid link pointing to .campaign/skills/ — safe to remove
	}

	if dryRun {
		fmt.Fprintf(out, "would remove: %s\n", dest)
		return nil
	}

	// Use os.Remove — never os.RemoveAll — to avoid following the symlink
	if err := os.Remove(dest); err != nil {
		return fmt.Errorf("remove symlink: %w", err)
	}

	fmt.Fprintf(out, "unlinked: %s\n", dest)
	return nil
}
