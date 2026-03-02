package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/skills"
)

var skillsLinkCmd = &cobra.Command{
	Use:   "link",
	Short: "Create symlinks from tool-specific paths to .campaign/skills/",
	Long: `Create symlinks from tool-specific paths to .campaign/skills/.

Use --tool to link a known tool ecosystem, or --path for a custom destination.
The symlink uses a relative path for portability across machines.

Examples:
  camp skills link --tool claude       Link .claude/skills -> .campaign/skills
  camp skills link --tool agents       Link .agents/skills -> .campaign/skills
  camp skills link --path custom/dir   Link custom/dir -> .campaign/skills
  camp skills link --tool claude -n    Dry run — show what would happen
  camp skills link --tool claude -f    Overwrite existing non-symlink target`,
	Args: cobra.NoArgs,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Agents use --tool or --path flags directly",
	},
	RunE: runSkillsLink,
}

func init() {
	skillsCmd.AddCommand(skillsLinkCmd)

	flags := skillsLinkCmd.Flags()
	flags.StringP("tool", "t", "", "Tool to link: claude, agents")
	flags.StringP("path", "p", "", "Custom destination path")
	flags.BoolP("force", "f", false, "Overwrite existing non-symlink destination")
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

	// Validate mutual exclusivity
	if tool != "" && destPath != "" {
		return fmt.Errorf("--tool and --path are mutually exclusive; use one or the other")
	}
	if tool == "" && destPath == "" {
		return fmt.Errorf("specify --tool <name> or --path <destination>\n\nAvailable tools: claude, agents")
	}

	// Locate .campaign/skills/
	skillsDir, err := skills.FindSkillsDir(ctx)
	if err != nil {
		return err
	}

	// Determine campaign root for resolving relative dest paths
	root, err := campaign.DetectCached(ctx)
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

	// Check current state of destination
	state, err := skills.CheckLinkState(dest, skillsDir)
	if err != nil {
		return err
	}

	switch state {
	case skills.StateValid:
		fmt.Fprintf(out, "already linked: %s\n", dest)
		return nil

	case skills.StateNotALink:
		if !force {
			return fmt.Errorf("destination exists and is not a symlink: %s\nUse --force to overwrite", dest)
		}
		fmt.Fprintf(errOut, "warning: overwriting existing path: %s\n", dest)

	case skills.StateBroken:
		// Broken symlink — we'll replace it
		if !dryRun {
			if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove broken symlink: %w", err)
			}
		}
	}

	// Compute relative symlink target
	relTarget, err := skills.RelativeSymlinkTarget(dest, skillsDir)
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Fprintf(out, "would create: %s -> %s\n", dest, relTarget)
		return nil
	}

	// Remove existing non-symlink if forced
	if state == skills.StateNotALink && force {
		if err := os.RemoveAll(dest); err != nil {
			return fmt.Errorf("remove existing path: %w", err)
		}
	}

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create parent directories: %w", err)
	}

	// Create symlink
	if err := os.Symlink(relTarget, dest); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied creating symlink (may require elevated privileges): %w", err)
		}
		return fmt.Errorf("create symlink: %w", err)
	}

	fmt.Fprintf(out, "linked: %s -> %s\n", dest, relTarget)
	return nil
}
