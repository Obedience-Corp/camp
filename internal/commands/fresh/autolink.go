package fresh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	initcmd "github.com/Obedience-Corp/camp/cmd/camp/init"
	projectlinked "github.com/Obedience-Corp/camp/cmd/camp/project/linked"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/starter"
	"github.com/Obedience-Corp/camp/internal/worktree"
)

// autoLinkRequest is what the fresh command knows when no camp was detected
// from the working directory.
type autoLinkRequest struct {
	// explicitTarget is true when the caller named what to cycle (a positional
	// project, --project, or --list). Naming a project only makes sense inside
	// a camp, so auto-link never applies.
	explicitTarget bool
	// noLink is --no-link.
	noLink bool
	// campaign is the --campaign selector, or "".
	campaign string
	// errOut receives selector match notes.
	errOut io.Writer
}

// unlinkedRepo is the git repository around the working directory.
type unlinkedRepo struct {
	// top is the work tree that contains the working directory. camp fresh
	// cycles this tree, as it does for a camp-managed worktree.
	top string
	// main is the repository's main checkout. It is the directory that gets
	// linked: a secondary worktree never receives a .camp attachment marker.
	main string
}

// autoLinkPlan is the checked decision about an unlinked repository. Building
// it writes nothing; apply performs the link.
type autoLinkPlan struct {
	repo unlinkedRepo
	// name is the project name fresh runs under.
	name string
	// campRoot is the camp the project belongs to, or will be linked into. For
	// a starter camp that does not exist yet it is where the starter will be
	// created.
	campRoot string
	// campName is the camp's name, for messages.
	campName string
	// linked is true when the main checkout is already a project of campRoot,
	// which happens when the working directory is a secondary worktree.
	linked bool
	// createStarter is true when no camp is registered and the starter camp
	// has to be created before linking.
	createStarter bool
}

// planAutoLink decides what camp fresh does when the working directory is not
// inside a camp. It returns the original "not in a camp" error unless every
// auto-link condition holds. --no-link still permits an existing main-checkout
// link to be used from a secondary worktree; it only blocks creating one.
func planAutoLink(ctx context.Context, detectErr error, req autoLinkRequest) (*autoLinkPlan, error) {
	notInCamp := camperrors.Wrap(detectErr, "not in a camp")
	if !errors.Is(detectErr, campaign.ErrNotInCampaign) || req.explicitTarget {
		return nil, notInCamp
	}
	repo, ok := detectUnlinkedRepo(ctx)
	if !ok {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, notInCamp
	}

	// A secondary worktree sits outside its main checkout, so detection from
	// the working directory misses a link the main checkout already has.
	root, err := campaign.Detect(ctx, repo.main)
	if err == nil {
		name, err := projectNameForPath(ctx, root, repo.main)
		if err != nil {
			return nil, err
		}
		if name == "" {
			return nil, notInCamp
		}
		return &autoLinkPlan{repo: repo, name: name, campRoot: root, campName: campNameAt(ctx, root), linked: true}, nil
	}
	if !errors.Is(err, campaign.ErrNotInCampaign) {
		return nil, camperrors.Wrap(err, "detect camp for main checkout")
	}
	if req.noLink {
		return nil, notInCamp
	}

	plan, err := resolveLinkCamp(ctx, req)
	if err != nil {
		return nil, err
	}
	plan.repo = repo
	plan.name = filepath.Base(repo.main)
	if !plan.createStarter {
		name, err := project.AvailableLinkName(plan.campRoot, plan.name)
		if err != nil {
			return nil, linkNameError(err, repo.main)
		}
		plan.name = name
	} else if err := project.ValidateProjectName(plan.name); err != nil {
		return nil, linkNameError(err, repo.main)
	}
	return plan, nil
}

// detectUnlinkedRepo finds the git work tree around the working directory and
// its main checkout. ok is false outside a git work tree and for a
// bare-repository layout, which has no main checkout to link.
func detectUnlinkedRepo(ctx context.Context) (unlinkedRepo, bool) {
	cwd, err := pathutil.LogicalCwd()
	if err != nil {
		return unlinkedRepo{}, false
	}
	top, err := git.Output(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil || top == "" {
		return unlinkedRepo{}, false
	}
	// Inside a nested submodule the project is the repository that owns it,
	// which is how a camp resolves the same working directory.
	for range maxSuperprojectDepth {
		super, err := git.Output(ctx, top, "rev-parse", "--show-superproject-working-tree")
		if err != nil || super == "" {
			break
		}
		top = super
	}
	entries, err := worktree.NewGitWorktree(top).List(ctx)
	if err != nil || len(entries) == 0 || entries[0].IsBare || entries[0].Path == "" {
		return unlinkedRepo{}, false
	}
	return unlinkedRepo{top: resolvedPath(top), main: resolvedPath(entries[0].Path)}, true
}

// maxSuperprojectDepth bounds the walk from a nested submodule to the
// repository that owns it.
const maxSuperprojectDepth = 16

func resolvedPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// projectNameForPath returns the name of the project in campRoot whose working
// directory is path, or "" when there is none.
func projectNameForPath(ctx context.Context, campRoot, path string) (string, error) {
	projects, err := project.List(ctx, campRoot)
	if err != nil {
		return "", camperrors.Wrap(err, "failed to list projects")
	}
	for _, proj := range projects {
		if resolvedPath(project.ResolveProjectPath(campRoot, proj)) == path {
			return proj.Name, nil
		}
	}
	return "", nil
}

func campNameAt(ctx context.Context, root string) string {
	if cfg, err := config.LoadCampaignConfig(ctx, root); err == nil && cfg.Name != "" {
		return cfg.Name
	}
	return filepath.Base(root)
}

// resolveLinkCamp picks the camp an unlinked repository is linked into:
// --campaign, then a registered camp named after the starter, then the
// starter camp for a user with no camps. Anything else stops and names
// --campaign rather than guessing.
func resolveLinkCamp(ctx context.Context, req autoLinkRequest) (*autoLinkPlan, error) {
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "load registry")
	}
	camps := reg.ListAll()
	slices.SortFunc(camps, func(a, b config.RegisteredCampaign) int {
		return strings.Compare(a.Name+"\x00"+a.ID, b.Name+"\x00"+b.ID)
	})

	if req.campaign != "" {
		selected, err := cmdutil.ResolveCampaignSelection(req.campaign, reg, req.errOut)
		if err != nil {
			return nil, err
		}
		return &autoLinkPlan{campRoot: selected.Path, campName: selected.Name}, nil
	}

	selected, err := defaultLinkCamp(camps)
	if err != nil {
		return nil, err
	}
	if selected != nil {
		return &autoLinkPlan{campRoot: selected.Path, campName: selected.Name}, nil
	}

	path, ok, err := starter.Pending(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, camperrors.New("this repository is not linked to a camp, and no camp is registered to link it into\n" +
			"Hint: Create a camp with 'camp create <name>', then run 'camp fresh --campaign <name>'")
	}
	return &autoLinkPlan{campRoot: path, campName: starter.Name, createStarter: true}, nil
}

// defaultLinkCamp applies the no-flag rule to the registered camps, which must
// be sorted. It returns the camp named after the starter when exactly one
// exists, nil when no camp is registered (the caller creates the starter), and
// an error that names --campaign in every other case.
func defaultLinkCamp(camps []config.RegisteredCampaign) (*config.RegisteredCampaign, error) {
	var named []config.RegisteredCampaign
	for _, c := range camps {
		if c.Name == starter.Name {
			named = append(named, c)
		}
	}
	switch {
	case len(named) == 1:
		return &named[0], nil
	case len(named) > 1:
		return nil, camperrors.Newf(
			"this repository is not linked to a camp, and more than one camp is named %q\n"+
				"Hint: Choose the camp to link it into with 'camp fresh --campaign <id>'\n\n%s",
			starter.Name, formatCampChoices(named, true))
	case len(camps) == 0:
		return nil, nil
	}
	return nil, camperrors.Newf(
		"this repository is not linked to a camp, and no camp is named %q\n"+
			"Hint: Choose the camp to link it into with 'camp fresh --campaign <name>'\n\n%s",
		starter.Name, formatCampChoices(camps, false))
}

func formatCampChoices(camps []config.RegisteredCampaign, withID bool) string {
	nameWidth := 0
	for _, c := range camps {
		nameWidth = max(nameWidth, len(c.Name))
	}
	var b strings.Builder
	b.WriteString("Registered camps:")
	for _, c := range camps {
		if withID {
			fmt.Fprintf(&b, "\n  %s  %-*s  %s", c.ID, nameWidth, c.Name, c.Path)
			continue
		}
		fmt.Fprintf(&b, "\n  %-*s  %s", nameWidth, c.Name, c.Path)
	}
	return b.String()
}

func linkNameError(err error, mainCheckout string) error {
	return camperrors.Wrapf(err,
		"cannot link %s automatically; link it yourself with 'camp project link %s --name <name>'",
		mainCheckout, mainCheckout)
}

// previewLine is what --dry-run prints in place of linking.
func (p *autoLinkPlan) previewLine() string {
	if p.createStarter {
		return fmt.Sprintf("Would link %s into your %s camp, creating the camp at %s.",
			p.linkLabel(), p.campName, p.campRoot)
	}
	return fmt.Sprintf("Would link %s into the %s camp at %s.", p.linkLabel(), p.campName, p.campRoot)
}

// notice is the one line printed after a link is created.
func (p *autoLinkPlan) notice(createdStarter bool) string {
	if createdStarter {
		return fmt.Sprintf("Linked %s into your %s camp, created at %s. Undo the link with 'camp project unlink'.",
			p.linkLabel(), p.campName, p.campRoot)
	}
	return fmt.Sprintf("Linked %s into the %s camp at %s. Undo with 'camp project unlink'.",
		p.linkLabel(), p.campName, p.campRoot)
}

// linkLabel names the project, and says so when a name collision gave it a
// different name from its folder.
func (p *autoLinkPlan) linkLabel() string {
	if base := filepath.Base(p.repo.main); base != p.name {
		return fmt.Sprintf("%s as %s", base, p.name)
	}
	return p.name
}

// apply creates the starter camp when needed, links the main checkout, and
// records the link in the camp. It returns the notice to print, which is empty
// when the camp turned out to hold a shortcut to this checkout already. errOut
// receives scaffold diagnostics and non-fatal link warnings.
func (p *autoLinkPlan) apply(ctx context.Context, errOut io.Writer) (string, error) {
	createdStarter := false
	if p.createStarter {
		result, err := initcmd.EnsureStarter(ctx, errOut)
		if err != nil {
			return "", camperrors.Wrap(err, "create starter camp")
		}
		switch result.Action {
		case "created":
			p.campRoot = result.Path
			createdStarter = true
		case "registered":
			// A starter left unregistered on disk was adopted, not created.
			p.campRoot = result.Path
		default:
			// Another process registered a camp or finished setup between the
			// check and now, for example a second camp fresh starting at the
			// same moment. Use the camp it created; otherwise do not guess.
			existing, err := registeredStarter(ctx)
			if err != nil {
				return "", err
			}
			if existing == nil {
				msg := "starter camp was not created"
				if result.Message != "" {
					msg += ": " + result.Message
				}
				return "", camperrors.New(msg + "\nHint: Choose the camp to link this repository into with 'camp fresh --campaign <name>'")
			}
			p.campRoot = existing.Path
		}
	}

	result, err := project.AddLinked(ctx, p.campRoot, p.repo.main, project.LinkOptions{
		Name: filepath.Base(p.repo.main), UniqueName: true,
	})
	if err != nil {
		// The camp already has a shortcut to this checkout: a concurrent run
		// linked it, or its .camp attachment marker was removed. It is linked
		// as far as the camp is concerned, so run under that name.
		var already *project.ErrProjectAlreadyLinked
		if errors.As(err, &already) {
			p.name = already.ExistingName
			p.linked = true
			return "", nil
		}
		return "", camperrors.Wrapf(err, "link %s into the %s camp", p.repo.main, p.campName)
	}
	p.name = result.Name
	p.linked = true
	for _, warning := range result.Warnings {
		_, _ = fmt.Fprintln(errOut, warning)
	}

	cfg, err := config.LoadCampaignConfig(ctx, p.campRoot)
	if err != nil {
		return "", camperrors.Wrap(err, "load camp config")
	}
	if commitResult := projectlinked.CommitLink(ctx, cfg, p.campRoot, result.Path, result.Name); commitResult.Err != nil {
		_, _ = fmt.Fprintf(errOut, "the link was created but not committed in the %s camp: %v\n", p.campName, commitResult.Err)
	}
	return p.notice(createdStarter), nil
}

// runAutoLink carries out a plan before the fresh cycle starts. previewOnly is
// true when --dry-run would create a camp that does not exist yet. Existing
// camps remain available for read-only workitem reports during previews.
func runAutoLink(ctx context.Context, plan *autoLinkPlan, flags freshFlagSet, errOut io.Writer) (previewOnly bool, err error) {
	if plan.linked {
		adoptCampRoot(ctx, plan.campRoot)
		return false, nil
	}
	if flags.dryRun {
		fmt.Printf("  %s\n\n", plan.previewLine())
		if plan.createStarter {
			return true, nil
		}
		adoptCampRoot(ctx, plan.campRoot)
		return false, nil
	}
	// Fail on what the cycle would refuse anyway before creating a camp or
	// writing a marker, so a run that cannot proceed leaves nothing behind.
	if err := preflightAutoLink(ctx, plan.repo.top, flags); err != nil {
		return false, err
	}
	notice, err := plan.apply(ctx, errOut)
	if err != nil {
		return false, err
	}
	adoptCampRoot(ctx, plan.campRoot)
	if notice != "" {
		fmt.Printf("  %s\n\n", notice)
	}
	return false, nil
}

// preflightAutoLink checks local refusal conditions before writing camp state.
// DefaultBranch may refresh Git's cached origin/HEAD, as in the fresh cycle.
func preflightAutoLink(ctx context.Context, path string, flags freshFlagSet) error {
	if flags.cleanupStack {
		if err := validateStackCleanupTarget(ctx, path, flags.branch, flags.allowDefaultTarget); err != nil {
			return err
		}
	}
	if err := freshSafetyChecks(ctx, path, false); err != nil {
		return err
	}
	if _, err := git.Output(ctx, path, "remote", "get-url", "origin"); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return camperrors.New("this repository has no 'origin' remote; camp fresh syncs to origin")
	}
	defaultBranch := git.DefaultBranch(ctx, path)
	if err := ctx.Err(); err != nil {
		return err
	}
	if defaultBranch == "" {
		return camperrors.Newf("could not determine default branch for %s", filepath.Base(path))
	}
	if _, err := resolveFreshSyncState(ctx, path, defaultBranch); err != nil {
		return camperrors.Wrap(err, "prepare fresh sync state")
	}
	return nil
}

// registeredStarter returns the one registered camp named after the starter,
// or nil when there is none or more than one.
func registeredStarter(ctx context.Context) (*config.RegisteredCampaign, error) {
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "load registry")
	}
	var found *config.RegisteredCampaign
	for _, c := range reg.ListAll() {
		if c.Name != starter.Name {
			continue
		}
		if found != nil {
			return nil, nil
		}
		found = &c
	}
	return found, nil
}

// adoptCampRoot makes the rest of this process resolve campRoot as its camp.
// Detection is cached per working directory and has already recorded that
// there is no camp here. After a link the working directory usually resolves
// through the new .camp attachment marker; a secondary worktree has no marker
// of its own, so CAMP_ROOT, the override for out-of-tree callers, pins the
// camp the repository's main checkout belongs to.
func adoptCampRoot(ctx context.Context, campRoot string) {
	campaign.ClearCache()
	if root, err := campaign.DetectCached(ctx); err == nil && resolvedPath(root) == resolvedPath(campRoot) {
		return
	}
	_ = os.Setenv(campaign.EnvCampaignRoot, campRoot)
	campaign.ClearCache()
}
