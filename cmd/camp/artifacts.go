package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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
	json   bool
	policy string
}

func init() {
	artifactsListCmd.Flags().BoolVar(&artifactsOpts.json, "json", false,
		"Output as JSON for scripting")
	artifactsAddCmd.Flags().StringVar(&artifactsOpts.policy, "policy", artifacts.PolicyAlways,
		"Sync policy: always (every peer sync) or on-demand (--artifacts-only)")

	artifactsCmd.AddCommand(artifactsListCmd)
	artifactsCmd.AddCommand(artifactsAddCmd)
	artifactsCmd.AddCommand(artifactsRemoveCmd)
	artifactsCmd.AddCommand(artifactsManifestCmd)
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
			Gitignored: verr == nil && isGitignored(cmd, campRoot, r.Path),
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

	normalized := artifacts.NormalizeRootPath(args[0])
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s Declared artifact root %s\n", ui.SuccessIcon(), normalized)

	if hasTrackedFiles(cmd, campRoot, normalized) {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"%s %s contains git-tracked files; artifact roots must not overlap git content (untrack them or pick another directory)\n",
			ui.WarningIcon(), normalized)
	}
	if !isGitignored(cmd, campRoot, normalized) {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"%s %s is not gitignored; add it to .gitignore so artifact bytes never land in git\n",
			ui.WarningIcon(), normalized)
	}
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

// isGitignored reports whether git ignores the path (the recommended state
// for artifact roots).
func isGitignored(cmd *cobra.Command, campRoot, rel string) bool {
	check := exec.CommandContext(cmd.Context(), "git", "-C", campRoot, "check-ignore", "-q", "--",
		filepath.FromSlash(artifacts.NormalizeRootPath(rel)))
	return check.Run() == nil
}

// hasTrackedFiles reports whether git tracks anything under the path (a
// class conflict: the same bytes would be both git content and artifacts).
func hasTrackedFiles(cmd *cobra.Command, campRoot, rel string) bool {
	ls := exec.CommandContext(cmd.Context(), "git", "-C", campRoot, "ls-files", "--",
		filepath.FromSlash(artifacts.NormalizeRootPath(rel)))
	out, err := ls.Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}
