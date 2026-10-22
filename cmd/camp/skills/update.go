package skills

import (
	"fmt"
	"io"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/scaffold"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
	"github.com/Obedience-Corp/camp/internal/version"
	"github.com/spf13/cobra"
)

var skillsUpdateCmd = &cobra.Command{
	Use:   "update [slug...]",
	Short: "Refresh shipped skill bundles to this camp version",
	Long: `Refresh the skill bundles camp ships in .campaign/skills/ to the content
this camp version ships. With no slug, every shipped bundle is checked.

Each shipped file is compared with this version's content and with
.campaign/skills/.shipped.yaml, the record of what camp last wrote:

  missing    written
  current    left alone
  outdated   unchanged since camp wrote it, so it is replaced
  modified   changed locally, so it is preserved unless --force
  unknown    differs with no record of what camp wrote, preserved unless --force
  symlink    a symlinked file or directory that differs; never replaced, even with --force

Every preserved file is reported with the command that replaces it. With
--force, camp first secures a one-command undo per bundle and prints it:
'git checkout HEAD -- .campaign/skills/<slug>' when the bundle is committed and
unchanged, otherwise a copy of the replaced files under .campaign/skills/.backup/.

Files camp does not ship, such as references/ or local bundles, are never touched.`,
	Example: `  camp skills update                          Refresh every shipped bundle
  camp skills update --dry-run                Show what would change
  camp skills update campaign-commit          Refresh one bundle
  camp skills update --force campaign-commit  Replace local changes in one bundle`,
	Args: cobra.ArbitraryArgs,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive; preserves local edits unless --force, which prints an undo",
	},
	RunE: runSkillsUpdate,
}

func init() {
	Cmd.AddCommand(skillsUpdateCmd)

	flags := skillsUpdateCmd.Flags()
	flags.BoolP("force", "f", false, "Also replace locally modified files, after printing an undo")
	flags.BoolP("dry-run", "n", false, "Show what would change without writing anything")
}

func runSkillsUpdate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}
	shipped, err := scaffold.ShippedSkills(ctx, root)
	if err != nil {
		return err
	}

	res, err := intskills.Update(ctx, intskills.UpdateOptions{
		Root:        root,
		Shipped:     shipped,
		Slugs:       args,
		Force:       force,
		DryRun:      dryRun,
		CampVersion: version.Version,
	})
	if res != nil {
		if printErr := printSkillsUpdate(cmd.OutOrStdout(), res); printErr != nil && err == nil {
			err = printErr
		}
	}
	return err
}

func printSkillsUpdate(out io.Writer, res *intskills.UpdateResult) error {
	var lines []string
	current, added := 0, false
	for _, f := range res.Files {
		switch {
		case f.Action == intskills.ActionKeep:
			current++
		case f.State == intskills.ShippedSymlink:
			lines = append(lines, fmt.Sprintf("left %s alone: it is a symlink, and camp never replaces symlinked skill files", f.RelPath()))
		case f.Action == intskills.ActionPreserve:
			lines = append(lines, fmt.Sprintf("preserved %s (%s); run 'camp skills update --force %s' to replace it",
				f.RelPath(), stateNote(f.State), f.Bundle))
		case res.DryRun:
			lines = append(lines, fmt.Sprintf("would %s %s (%s)", f.Action, f.RelPath(), stateNote(f.State)))
		case f.Written:
			lines = append(lines, fmt.Sprintf("%s %s (%s)", pastTense(f.Action), f.RelPath(), stateNote(f.State)))
			added = added || f.Action == intskills.ActionAdd
		}
	}
	for _, u := range res.Undo {
		if u.BackupDir != "" {
			lines = append(lines, fmt.Sprintf("backed up replaced %s files to %s", u.Bundle, u.BackupDir))
		}
		lines = append(lines, fmt.Sprintf("undo %s: %s (camp skills status then shows %s as modified)", u.Bundle, u.Command, u.Bundle))
	}
	lines = append(lines, fmt.Sprintf("%d file(s) already current", current))
	switch {
	case res.DryRun:
		lines = append(lines, "dry run: nothing written")
	case res.ManifestWritten:
		lines = append(lines, "recorded .campaign/skills/"+intskills.ManifestFileName)
	}
	if added {
		lines = append(lines, "run 'camp skills link' to project added bundles into tool skill directories")
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}

func pastTense(a intskills.FileAction) string {
	switch a {
	case intskills.ActionAdd:
		return "added"
	case intskills.ActionUpdate:
		return "updated"
	default:
		return "replaced"
	}
}

func stateNote(s intskills.ShippedState) string {
	switch s {
	case intskills.ShippedMissing:
		return "missing"
	case intskills.ShippedOutdated:
		return "outdated"
	case intskills.ShippedModified:
		return "changed locally"
	default:
		return "differs, no record of what camp shipped"
	}
}
