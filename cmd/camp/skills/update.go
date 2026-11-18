package skills

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/scaffold"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
	"github.com/Obedience-Corp/camp/internal/version"
	"github.com/spf13/cobra"
)

// SkillsUpdateJSONVersion is the schema of `camp skills update --json`.
const SkillsUpdateJSONVersion = "skills-update/v1alpha1"

var skillsUpdateJSON bool

var skillsUpdateCmd = &cobra.Command{
	Use:   "update [slug...]",
	Short: "Refresh shipped skill bundles to this camp version",
	Long: `Refresh the skill bundles camp ships in .campaign/skills/ to the content
this camp version ships. With no slug, every shipped bundle is checked.

Each shipped file is compared with this version's content and with
.campaign/skills/.shipped.yaml, the record of what camp last wrote. Camps
created before that record existed have none, so for a file it does not cover
camp reads git history instead: a file whose only commit is camp init's or
camp repair's, unchanged since, still holds what camp wrote and is replaced
without --force. A file first committed any other way stays unknown.

  missing    written
  current    left alone
  outdated   unchanged since camp wrote it, per the record or git, so it is replaced
  modified   changed locally, so it is preserved unless --force
  unknown    differs and neither the record nor git shows what camp wrote;
             preserved unless --force
  symlink    a symlinked file or directory that differs; never replaced, even with --force

Every preserved file is reported with the command that replaces it. A file
replaced on git evidence alone is reported with the command that restores it,
'git checkout HEAD -- <file>...'. With --force, camp first secures a
one-command undo per bundle and prints it:
'git checkout HEAD -- .campaign/skills/<slug>' when the bundle is committed and
unchanged, otherwise a copy of the replaced files under .campaign/skills/.backup/.

Files camp does not ship, such as references/ or local bundles, are never touched.

A plugin can offer skill bundles too. Naming one, such as festival-app, asks
the camp-app plugin on PATH for it ('camp-app skill --json', with a 5 second
limit) and writes it the same way, recording the camp-app version that wrote
it. camp-app is asked only when a request names a bundle camp does not ship;
a refresh with no slug never asks it. A bundle written by a newer camp-app
than this machine has is left alone (ahead). 'camp skills decline <slug>'
removes a plugin bundle and keeps it out of this camp.

  --auto   an automatic request: adds or refreshes, leaves a declined bundle
           alone, cannot be combined with --force, and never fails because
           camp-app is missing or too old
  --link   then links the named bundles into .claude/skills and .agents/skills
           where those folders already hold camp's links; never creates one
  --json   prints one result, schema skills-update/v1alpha1`,
	Example: `  camp skills update                          Refresh every shipped bundle
  camp skills update --dry-run                Show what would change
  camp skills update campaign-commit          Refresh one bundle
  camp skills update --force campaign-commit  Replace local changes in one bundle
  camp skills update festival-app             Add or refresh the Festival app skill from camp-app
  camp skills update festival-app --auto --link --json
                                              What the Festival app runs on camp open`,
	Args: cobra.ArbitraryArgs,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive; preserves local edits unless --force, which prints an undo",
	},
	RunE: jsoncontract.RunE(SkillsUpdateJSONVersion, func() bool { return skillsUpdateJSON }, runSkillsUpdate),
}

func init() {
	Cmd.AddCommand(skillsUpdateCmd)

	flags := skillsUpdateCmd.Flags()
	flags.BoolP("force", "f", false, "Also replace locally modified files, after printing an undo")
	flags.BoolP("dry-run", "n", false, "Show what would change without writing anything")
	flags.Bool("auto", false, "Automatic request: leave declined bundles alone; refuses --force")
	flags.Bool("link", false, "Link the named bundles into tool folders that already hold camp's links")
	flags.BoolVar(&skillsUpdateJSON, "json", false, "Print one result as JSON")
	skillsUpdateCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(SkillsUpdateJSONVersion, func() bool { return skillsUpdateJSON }))
}

func runSkillsUpdate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	req := updateRequest{args: args}
	req.force, _ = cmd.Flags().GetBool("force")
	req.dryRun, _ = cmd.Flags().GetBool("dry-run")
	req.auto, _ = cmd.Flags().GetBool("auto")
	link, _ := cmd.Flags().GetBool("link")
	if req.auto && req.force {
		return usageError(cmd, skillsUpdateJSON, "--auto", "--auto cannot be combined with --force: an automatic request never replaces local changes")
	}

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}
	shipped, err := scaffold.ShippedSkills(ctx, root)
	if err != nil {
		return err
	}
	core := updateCore{sources: campPlanSources(), update: intskills.Update}
	plan, res, err := core.run(ctx, root, shipped, req)
	var links []intskills.BundleLink
	if err == nil && link {
		skillsDir := filepath.Join(root, campaign.CampaignDir, intskills.SkillsSubdir)
		links, err = intskills.LinkBundles(root, skillsDir, linkSlugs(skillsDir, args, res, plan), req.dryRun)
	}
	if res == nil || (skillsUpdateJSON && err != nil) {
		return err
	}
	out := updateOutput{res: res, plan: plan, links: links, link: link}
	if printErr := out.print(cmd.OutOrStdout(), cmd.ErrOrStderr(), skillsUpdateJSON); printErr != nil && err == nil {
		err = printErr
	}
	return err
}

// updateRequest is one `camp skills update` invocation.
type updateRequest struct {
	args                []string
	auto, force, dryRun bool
}

// updateCore plans a request and runs Update with the scope it settles on.
// Its sources and its update function are dependencies, so the decision of
// what Update gets, and whether it runs at all, is testable without a camp.
type updateCore struct {
	sources planSources
	update  func(context.Context, intskills.UpdateOptions) (*intskills.UpdateResult, error)
}

func (c updateCore) run(ctx context.Context, root string, shipped []intskills.ShippedFile, req updateRequest) (pluginPlan, *intskills.UpdateResult, error) {
	plan, err := planPluginSlugs(ctx, c.sources, root, shipped, req.args, req.auto)
	if err != nil {
		return plan, nil, err
	}
	slugs, run := updateScope(req.args, plan)
	if !run {
		return plan, &intskills.UpdateResult{DryRun: req.dryRun}, nil
	}
	res, err := c.update(ctx, intskills.UpdateOptions{
		Root: root, Shipped: shipped, Plugins: plan.bundles, Slugs: slugs,
		Auto: req.auto, Force: req.force, DryRun: req.dryRun, CampVersion: version.Version,
	})
	return plan, res, err
}

// usageError refuses an invocation shape with exit code 2, as a JSON
// envelope when asked for JSON.
func usageError(cmd *cobra.Command, asJSON bool, field, message string) error {
	err := camperrors.NewValidation(field, message, nil)
	if asJSON {
		return err
	}
	cmd.SilenceUsage = true
	if _, printErr := fmt.Fprintln(cmd.ErrOrStderr(), "Error: "+message); printErr != nil {
		return printErr
	}
	return camperrors.NewCommand(cmd.CommandPath(), 2, "", err)
}
