package skills

import (
	"context"
	"fmt"
	"io"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
)

// linkAllWorktrees projects skill bundles into every projects/worktrees/*/*
// checkout under the campaign. ctx is used for campaign config load so
// cancellation from the CLI propagates.
func linkAllWorktrees(ctx context.Context, out, errOut io.Writer, root, skillsDir string, force, dryRun bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		// Paths defaults still work when campaign config is missing/unloadable.
		cfg = &config.CampaignConfig{}
	}
	resolver := paths.NewResolver(root, cfg.Paths())
	wtRoot := resolver.Worktrees()

	results, err := intskills.LinkAllWorktrees(root, wtRoot, skillsDir, dryRun, force, errOut)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		if _, err := fmt.Fprintf(out, "no project worktrees under %s\n", wtRoot); err != nil {
			return err
		}
		return nil
	}

	verb := "projected"
	if dryRun {
		verb = "would project"
	}
	var conflicts int
	var conflictNames []string
	var hardErrs int
	for _, res := range results {
		if res.Err != nil {
			hardErrs++
			if _, err := fmt.Fprintf(errOut, "worktree %s: %v\n", res.RelPath, res.Err); err != nil {
				return err
			}
			continue
		}
		s := res.Agents
		if _, err := fmt.Fprintf(out, "%s %d skill bundle(s) into worktree %s (created=%d replaced=%d unchanged=%d)\n",
			verb, s.Created+s.Replaced+s.AlreadyLinked, res.RelPath, s.Created, s.Replaced, s.AlreadyLinked); err != nil {
			return err
		}
		conflicts += s.Conflicts + res.Claude.Conflicts
		conflictNames = append(conflictNames, s.ConflictNames...)
		conflictNames = append(conflictNames, res.Claude.ConflictNames...)
	}
	if hardErrs > 0 {
		return camperrors.Newf("worktree skill projection failed for %d worktree(s)", hardErrs)
	}
	if conflicts > 0 {
		return camperrors.Newf("worktree projection incomplete: %d conflicting skill path(s): %v", conflicts, conflictNames)
	}
	return nil
}
