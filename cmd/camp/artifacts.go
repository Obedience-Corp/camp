package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/peer"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var artifactsCmd = &cobra.Command{
	Use:   "artifacts",
	Short: "Manage declared artifact roots (.campaign/artifacts.yaml)",
	Long: `Manage the campaign's declared artifact roots: directories of heavy non-git
payloads (media, renders, datasets) that 'camp sync --from <machine>' moves
between your machines with rsync instead of git.

The declaration file (.campaign/artifacts.yaml) is committed, so every
machine knows what belongs to the campaign. Declared roots should be
gitignored: a root that is also git-tracked would make the same bytes both
git content and artifact content. Manifests and per-peer sync snapshots are
machine-local derived state under .campaign/cache (gitignored).`,
	Example: `  camp artifacts list
  camp artifacts add media/renders
  camp artifacts add datasets --policy on-demand
  camp artifacts remove media/renders
  camp artifacts manifest media/renders`,
}

var artifactsListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List declared artifact roots",
	RunE:    runArtifactsList,
}

var artifactsAddCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Declare an artifact root",
	Long: `Declare a campaign-relative directory as an artifact root.

Policy 'always' (default) syncs the root on every 'camp sync --from
<machine>'; 'on-demand' syncs it only when artifacts are requested
explicitly (--artifacts-only).`,
	Args: cobra.ExactArgs(1),
	RunE: runArtifactsAdd,
}

var artifactsRemoveCmd = &cobra.Command{
	Use:     "remove <path>",
	Aliases: []string{"rm"},
	Short:   "Remove an artifact root declaration",
	Long:    `Remove a declared artifact root. Files on disk are not touched.`,
	Args:    cobra.ExactArgs(1),
	RunE:    runArtifactsRemove,
}

var artifactsManifestCmd = &cobra.Command{
	Use:   "manifest <path>",
	Short: "Print a declared root's manifest as JSON",
	Long: `Walk a declared artifact root and print its manifest (relative path, size,
mtime per file) as JSON. This is the same shape sync snapshots use, so it is
useful for scripting and for comparing roots across machines.`,
	Args: cobra.ExactArgs(1),
	RunE: runArtifactsManifest,
}

var artifactsOpts struct {
	json        bool
	policy      string
	dryRun      bool
	noGitignore bool
}

func init() {
	artifactsListCmd.Flags().BoolVar(&artifactsOpts.json, "json", false,
		"Output as JSON for scripting")
	artifactsAddCmd.Flags().StringVar(&artifactsOpts.policy, "policy", artifacts.PolicyAlways,
		"Sync policy: always (every peer sync) or on-demand (--artifacts-only)")
	artifactsAddCmd.Flags().BoolVar(&artifactsOpts.dryRun, "dry-run", false,
		"Report what declaring this root would cover; write nothing")
	artifactsAddCmd.Flags().BoolVar(&artifactsOpts.noGitignore, "no-gitignore", false,
		"Declare the root without adding its .gitignore rule")

	artifactsCmd.AddCommand(artifactsListCmd)
	artifactsCmd.AddCommand(artifactsAddCmd)
	artifactsCmd.AddCommand(artifactsRemoveCmd)
	artifactsCmd.AddCommand(artifactsManifestCmd)
	artifactsResolveCmd.Flags().BoolVar(&resolveOpts.takeLocal, "take-local", false,
		"Keep your copy; pins that path local for this peer")
	artifactsResolveCmd.Flags().BoolVar(&resolveOpts.takePeer, "take-peer", false,
		"Fetch the peer's copy of that path and record it as agreed")
	artifactsResolveCmd.Flags().BoolVar(&resolveOpts.list, "list", false,
		"List open conflicts with the peer and change nothing")
	artifactsResolveCmd.Flags().StringVar(&resolveOpts.from, "from", "",
		"Machine id the conflict is with (required; conflicts are per peer)")
	artifactsResolveCmd.Flags().BoolVar(&resolveOpts.json, "json", false,
		"Output as JSON")
	artifactsCmd.AddCommand(artifactsResolveCmd)
	rootCmd.AddCommand(artifactsCmd)
	artifactsCmd.GroupID = "campaign"
}

type artifactRootJSON struct {
	Path       string `json:"path"`
	Policy     string `json:"policy"`
	Exists     bool   `json:"exists"`
	Gitignored bool   `json:"gitignored"`
}

type artifactsListOutput struct {
	Version int                `json:"version"`
	Roots   []artifactRootJSON `json:"roots"`
}

func runArtifactsList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}
	cfg, err := artifacts.Load(campRoot)
	if err != nil {
		return err
	}

	out := artifactsListOutput{Version: 1, Roots: []artifactRootJSON{}}
	for _, r := range cfg.Roots {
		// Validate before stat: a hand-edited artifacts.yaml with a `../..`
		// root would otherwise make this read-only command stat and report on
		// files outside the campaign. Invalid roots are listed (so the user
		// sees the bad declaration) but never touched on disk.
		normalized, verr := artifacts.EnsureRootWithin(campRoot, r.Path)
		exists := false
		if verr == nil {
			abs := filepath.Join(campRoot, filepath.FromSlash(normalized))
			info, statErr := os.Stat(abs)
			exists = statErr == nil && info.IsDir()
		} else {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "  warning: artifact root %q is invalid and was skipped: %v\n", r.Path, verr)
		}
		out.Roots = append(out.Roots, artifactRootJSON{
			Path:       artifacts.NormalizeRootPath(r.Path),
			Policy:     r.EffectivePolicy(),
			Exists:     exists,
			Gitignored: verr == nil && artifacts.IsGitignored(cmd.Context(), campRoot, r.Path),
		})
	}

	if artifactsOpts.json {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	if len(out.Roots) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No artifact roots declared. Add one with 'camp artifacts add <path>'.")
		return nil
	}
	for _, r := range out.Roots {
		status := ui.SuccessIcon()
		notes := []string{r.Policy}
		if !r.Exists {
			status = ui.WarningIcon()
			notes = append(notes, "missing locally")
		}
		if !r.Gitignored {
			status = ui.WarningIcon()
			notes = append(notes, "not gitignored")
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s %s (%s)\n", status, r.Path, strings.Join(notes, ", "))
	}
	return nil
}

func runArtifactsAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}
	// Validate before doing any work. Add validates too, but --dry-run never
	// reaches Add, and reporting on a declaration that could never succeed
	// would be worse than a plain error.
	if err := artifacts.ValidateRootPath(args[0]); err != nil {
		return err
	}
	if err := artifacts.ValidatePolicy(artifactsOpts.policy); err != nil {
		return err
	}
	normalized := artifacts.NormalizeRootPath(args[0])

	// Survey before declaring so --dry-run and the mixed-root report share one
	// source of truth, and so a user asking "what would this cover" gets the
	// same numbers the real run would act on.
	survey, err := artifacts.SurveyRoot(ctx, campRoot, normalized)
	if err != nil {
		return err
	}

	if artifactsOpts.dryRun {
		printArtifactsDryRun(cmd, normalized, survey)
		return nil
	}

	cfg, err := artifacts.Load(campRoot)
	if err != nil {
		return err
	}
	root := artifacts.Root{Path: args[0], Policy: artifactsOpts.policy}
	if root.Policy == artifacts.PolicyAlways {
		root.Policy = "" // default stays implicit in the file
	}
	if err := cfg.Add(root); err != nil {
		return err
	}
	if err := cfg.Save(campRoot); err != nil {
		return err
	}

	if survey.Mixed() {
		printArtifactsMixed(cmd, normalized, survey)
		return nil
	}
	return reportCleanRoot(cmd, campRoot, normalized)
}

// printArtifactsDryRun renders the pre-declaration report: what the root holds
// and how declaring it would split those files.
func printArtifactsDryRun(cmd *cobra.Command, normalized string, survey artifacts.RootSurvey) {
	out := cmd.OutOrStdout()
	policy := artifactsOpts.policy
	if policy == "" {
		policy = artifacts.PolicyAlways
	}
	_, _ = fmt.Fprintf(out, "%s would be declared as an artifact root (policy: %s)\n", normalized, policy)
	// Counts are right-aligned to a common width so the breakdown lines up
	// under the total, per design doc 03's sample. Wider counts widen the
	// column rather than breaking the alignment.
	_, _ = fmt.Fprintf(out, "%7s files, %s total\n",
		ui.FormatCount(survey.TotalFiles()), ui.FormatBytes(survey.TotalBytes))
	_, _ = fmt.Fprintf(out, "%7s tracked      excluded from artifact sync\n", ui.FormatCount(survey.Tracked))
	_, _ = fmt.Fprintf(out, "%7s untracked    the artifact set\n", ui.FormatCount(survey.Untracked))
	_, _ = fmt.Fprintf(out, "%7s ignored\n", ui.FormatCount(survey.Ignored))
	_, _ = fmt.Fprintf(out, "\nNothing was written. Re-run without --dry-run to declare it.\n")
}

// printArtifactsMixed reports a root that holds tracked content. Camp never
// gitignores such a root: the rule would hide tracked files from git without
// untracking them, so the split is reported and the file is left alone.
func printArtifactsMixed(cmd *cobra.Command, normalized string, survey artifacts.RootSurvey) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "%s Declared artifact root %s (mixed)\n", ui.SuccessIcon(), normalized)
	_, _ = fmt.Fprintf(out, "  %s git-tracked files stay with git and are excluded from artifact sync\n",
		ui.FormatCount(survey.Tracked))
	// The byte figure describes the artifact set, not the directory: tracked
	// scripts sitting beside the footage are git's and rsync never carries them.
	_, _ = fmt.Fprintf(out, "  %s untracked files (%s) are the artifact set\n",
		ui.FormatCount(survey.Untracked), ui.FormatBytes(survey.UntrackedBytes))
	_, _ = fmt.Fprintf(out, "  .gitignore not modified: ignoring this root would hide tracked content\n")
}

// reportCleanRoot handles a root with no tracked content: the case where a
// blanket ignore rule is exactly right and camp can act without judgment.
func reportCleanRoot(cmd *cobra.Command, campRoot, normalized string) error {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "%s Declared artifact root %s\n", ui.SuccessIcon(), normalized)

	if artifactsOpts.noGitignore {
		if !artifacts.IsGitignored(cmd.Context(), campRoot, normalized) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"%s %s is not gitignored; add it to .gitignore so artifact bytes never land in git\n",
				ui.WarningIcon(), normalized)
		}
		return nil
	}

	if artifacts.IsGitignored(cmd.Context(), campRoot, normalized) {
		// Already ignored, by an existing rule or a parent's. Say so rather
		// than printing nothing, so the absence of an "Added ..." line does
		// not read as camp having silently skipped the step.
		_, _ = fmt.Fprintf(out, "  Already gitignored; .gitignore not modified\n")
		return nil
	}

	rule := normalized + "/"
	wrote, err := fsutil.AppendGitignoreEntryIfMissing(
		filepath.Join(campRoot, ".gitignore"), rule, cmdutil.GitignoreRuleComment)
	if err != nil {
		return camperrors.Wrapf(err, "add %q to .gitignore", rule)
	}
	if !wrote {
		return nil
	}

	_, _ = fmt.Fprintf(out, "%s Added '%s' to .gitignore\n", ui.SuccessIcon(), rule)
	_, _ = fmt.Fprintf(out, "  Everything in this root now lives outside git and syncs between your\n")
	_, _ = fmt.Fprintf(out, "  machines with 'camp sync'. Undo: camp artifacts remove %s\n", normalized)
	return nil
}

func runArtifactsRemove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}
	cfg, err := artifacts.Load(campRoot)
	if err != nil {
		return err
	}
	if !cfg.Remove(args[0]) {
		return camperrors.Newf("artifact root %q is not declared", args[0])
	}
	if err := cfg.Save(campRoot); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s Removed artifact root %s (files on disk untouched)\n",
		ui.SuccessIcon(), artifacts.NormalizeRootPath(args[0]))
	return nil
}

func runArtifactsManifest(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}
	cfg, err := artifacts.Load(campRoot)
	if err != nil {
		return err
	}
	root, found := cfg.Find(args[0])
	if !found {
		return camperrors.Newf("artifact root %q is not declared (see 'camp artifacts list')", args[0])
	}
	// Validate before walking: a hand-edited root must not let this read-only
	// command build a manifest of files outside the campaign.
	if _, err := artifacts.EnsureRootWithin(campRoot, root.Path); err != nil {
		return camperrors.Wrapf(err, "artifact root %q", root.Path)
	}
	m, err := artifacts.BuildManifest(ctx, campRoot, root.Path)
	if err != nil {
		return err
	}
	data, err := m.EncodeJSON()
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(data)
	return err
}

// printResolveJSON writes v as indented JSON, matching the other artifacts
// subcommands' output shape.
func printResolveJSON(cmd *cobra.Command, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "encode resolve json")
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

// resolveOpts holds the flags for `camp artifacts resolve`.
var resolveOpts struct {
	takeLocal bool
	takePeer  bool
	list      bool
	from      string
	json      bool
}

var artifactsResolveCmd = &cobra.Command{
	Use:   "resolve [path]",
	Short: "Resolve an artifact conflict kept by no-clobber protection",
	Long: `Resolve one reported artifact conflict.

A sync never overwrites a local file whose bytes differ from the last state
agreed with a peer, and that protection is sticky: it survives every later
sync. This is how you clear it deliberately, instead of deleting the local
file to make the protection go away.

  --list          show the open conflicts with a peer (changes nothing)
  --take-local    keep your copy; that path is then pinned local for this
                  peer, so later peer changes to it will not arrive on their
                  own. Run resolve --take-peer if you want them.
  --take-peer     fetch the peer's copy of that one path, install it, and
                  record it as agreed

There is no --all: resolving in bulk is exactly what the sticky conflict
exists to prevent. Loop the per-path form if you really mean it.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runArtifactsResolve,
}

func runArtifactsResolve(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}
	if resolveOpts.from == "" {
		return camperrors.New("--from <machine> is required: conflicts are per peer")
	}
	if resolveOpts.list {
		return runArtifactsResolveList(ctx, cmd, campRoot)
	}

	action, err := resolveActionFromFlags()
	if err != nil {
		return err
	}
	if len(args) != 1 {
		return camperrors.New("resolve needs exactly one <path> (or --list)")
	}

	// take-local is a local decision and must work with the peer offline;
	// only take-peer needs to reach it for bytes.
	var src *peer.Source
	if action == artifacts.ResolveTakePeer {
		src, err = peer.FromMachine(ctx, resolveOpts.from, filepath.Base(campRoot))
		if err != nil {
			return camperrors.Wrapf(err, "--take-peer needs machine %q", resolveOpts.from)
		}
	}

	result, err := artifacts.Resolve(ctx, campRoot, src, resolveOpts.from, args[0], action)
	if err != nil {
		return err
	}
	if resolveOpts.json {
		return printResolveJSON(cmd, result)
	}
	printResolveResult(cmd, result)
	return nil
}

// resolveActionFromFlags turns the mutually exclusive flags into an action.
func resolveActionFromFlags() (artifacts.ResolveAction, error) {
	switch {
	case resolveOpts.takeLocal && resolveOpts.takePeer:
		return "", camperrors.New("--take-local and --take-peer are mutually exclusive")
	case resolveOpts.takeLocal:
		return artifacts.ResolveTakeLocal, nil
	case resolveOpts.takePeer:
		return artifacts.ResolveTakePeer, nil
	default:
		return "", camperrors.New("choose a side: --take-local or --take-peer (or --list to look first)")
	}
}

func runArtifactsResolveList(ctx context.Context, cmd *cobra.Command, campRoot string) error {
	conflicts, err := artifacts.Conflicts(ctx, campRoot, resolveOpts.from)
	if err != nil {
		return err
	}
	if resolveOpts.json {
		return printResolveJSON(cmd, map[string]any{"peer": resolveOpts.from, "conflicts": conflicts})
	}
	out := cmd.OutOrStdout()
	if len(conflicts) == 0 {
		_, _ = fmt.Fprintf(out, "%s No open conflicts with %s\n", ui.SuccessIcon(), resolveOpts.from)
		return nil
	}
	_, _ = fmt.Fprintf(out, "Open conflicts with %s:\n\n", resolveOpts.from)
	for _, c := range conflicts {
		_, _ = fmt.Fprintf(out, "  %s\n", filepath.ToSlash(filepath.Join(c.Root, c.Path)))
		_, _ = fmt.Fprintf(out, "      yours: %d bytes    last agreed: %d bytes (%s)\n",
			c.LocalSize, c.AgreedSize, c.AgreedAt.Format("2006-01-02 15:04"))
	}
	_, _ = fmt.Fprintf(out, "\nResolve with:\n  camp artifacts resolve <path> --from %s --take-local|--take-peer\n",
		resolveOpts.from)
	return nil
}

func printResolveResult(cmd *cobra.Command, r *artifacts.ResolveResult) {
	out := cmd.OutOrStdout()
	full := filepath.ToSlash(filepath.Join(r.Root, r.Path))
	if r.Action == artifacts.ResolveTakeLocal {
		_, _ = fmt.Fprintf(out, "%s Kept your copy of %s\n", ui.SuccessIcon(), full)
		_, _ = fmt.Fprintf(out, "  Pinned local for %s: later changes to it on that machine will not arrive on their own.\n", r.Peer)
		_, _ = fmt.Fprintf(out, "  Take them later with: camp artifacts resolve %s --from %s --take-peer\n", full, r.Peer)
		return
	}
	_, _ = fmt.Fprintf(out, "%s Took %s's copy of %s\n", ui.SuccessIcon(), r.Peer, full)
	if r.NewBaseline != nil {
		_, _ = fmt.Fprintf(out, "  Now agreed at %d bytes; the conflict is cleared.\n", r.NewBaseline.Size)
	}
}
