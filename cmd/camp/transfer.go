package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/Obedience-Corp/camp/internal/transfer"
	"github.com/spf13/cobra"
)

var transferCmd = &cobra.Command{
	Use:   "transfer <src> <dest>",
	Short: "Copy files between campaigns (and machines)",
	Long: `Copy files between campaigns, and between this machine and a registered
fleet machine.

Transfer always copies — it never moves or deletes the source.

Local forms:
  campaign:path     another registered campaign on this machine
  path              relative to the current campaign root
  local:campaign:path
                    force the campaign reading when campaign name collides
                    with a registered machine id

Machine forms (one side only; both-remote is refused):
  machine:campaign:path
                    file on a machine registered in ~/.obey/machines.yaml

See docs/transfer.md for the full grammar, transport, and skew guidance.

At least one side must reference a different campaign or machine. For copies
within the same campaign on this machine, use 'camp copy' instead.`,
	Example: `  camp transfer docs/my-doc.md other-campaign:docs/my-doc.md     # local push
  camp transfer other-campaign:docs/my-doc.md docs/              # local pull
  camp transfer other:festivals/plan.md festivals/planned/       # pull into dir
  camp transfer docs/x.md archdtop:obey-campaign:docs/x.md       # push to machine
  camp transfer archdtop:obey-campaign:docs/x.md docs/x.md       # pull from machine
  camp transfer local:other:docs/x.md archdtop:camp:docs/x.md    # local: escape hatch`,
	Args: cobra.ExactArgs(2),
	RunE: runTransfer,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 2 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeTransferArg(cmd, toComplete)
	},
}

func init() {
	rootCmd.AddCommand(transferCmd)
	transferCmd.GroupID = "global"
	transferCmd.Flags().BoolP("force", "f", false, "Overwrite destination without prompting")
}

func runTransfer(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	force, _ := cmd.Flags().GetBool("force")

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	src, err := transfer.ParseEndpointDefault(ctx, args[0])
	if err != nil {
		return camperrors.Wrap(err, "resolve source")
	}
	dest, err := transfer.ParseEndpointDefault(ctx, args[1])
	if err != nil {
		return camperrors.Wrap(err, "resolve destination")
	}
	for _, e := range []transfer.Endpoint{src, dest} {
		if e.Shadowed {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), transfer.ShadowNote(e.Machine))
		}
	}
	if src.IsRemote() && dest.IsRemote() {
		// Brokering a copy between two far machines would need them to reach
		// each other, which D003 says camp diagnoses rather than manages.
		return transfer.BothRemoteError(src, dest)
	}
	if src.IsRemote() || dest.IsRemote() {
		return runRemoteTransfer(ctx, cmd, root, src, dest, force)
	}

	srcPath, err := transfer.ResolveCrossCampaignPath(ctx, root, src.Spec)
	if err != nil {
		return camperrors.Wrap(err, "resolve source")
	}
	if err := transfer.ValidatePathExists(srcPath); err != nil {
		return camperrors.Wrap(err, "source")
	}

	destPath, err := transfer.ResolveCrossCampaignPath(ctx, root, dest.Spec)
	if err != nil {
		return camperrors.Wrap(err, "resolve destination")
	}

	// If dest is a directory or ends with /, place source inside it
	destArg := dest.Spec
	// Strip campaign prefix for trailing slash check
	if idx := strings.Index(destArg, ":"); idx >= 0 {
		destArg = destArg[idx+1:]
	}
	if transfer.IsDestDir(destPath) || transfer.IsDestDir(destArg) {
		destPath = filepath.Join(destPath, filepath.Base(srcPath))
	}

	if !force {
		if _, err := os.Stat(destPath); err == nil {
			return camperrors.Newf("destination %q already exists (use --force to overwrite)", destPath)
		}
	}

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return camperrors.Wrap(err, "stat source")
	}

	if srcInfo.IsDir() {
		if err := transfer.CopyDir(srcPath, destPath); err != nil {
			return camperrors.Wrap(err, "transfer directory")
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return camperrors.Wrap(err, "create destination directory")
		}
		if err := transfer.CopyFile(srcPath, destPath); err != nil {
			return camperrors.Wrap(err, "transfer file")
		}
	}

	fmt.Printf("Transferred %s → %s\n", args[0], args[1])
	return nil
}

// completeTransferArg provides tab completion for campaign:path arguments.
func completeTransferArg(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()

	// Check if we're completing after the colon (file paths within a campaign)
	if idx := strings.Index(toComplete, ":"); idx >= 0 {
		campaignName := toComplete[:idx]
		pathPrefix := toComplete[idx+1:]

		reg, err := config.LoadRegistry(ctx)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		entry, ok := reg.Get(campaignName)
		if !ok {
			return nil, cobra.ShellCompDirectiveError
		}

		return completeCampaignPath(entry.Path, pathPrefix, toComplete[:idx+1])
	}

	// Before colon: complete campaign names with trailing colon
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	toCompleteLower := strings.ToLower(toComplete)
	var names []string
	for _, c := range reg.ListAll() {
		if strings.HasPrefix(strings.ToLower(c.Name), toCompleteLower) {
			names = append(names, c.Name+":")
		}
	}
	return names, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

// completeCampaignPath completes file paths within a campaign directory.
func completeCampaignPath(campRoot, pathPrefix, colonPrefix string) ([]string, cobra.ShellCompDirective) {
	searchDir := filepath.Join(campRoot, pathPrefix)
	dirToRead := searchDir
	prefix := pathPrefix

	var filter string
	if pathPrefix != "" && !strings.HasSuffix(pathPrefix, "/") {
		dirToRead = filepath.Dir(searchDir)
		filter = filepath.Base(pathPrefix)
		prefix = filepath.Dir(pathPrefix)
		if prefix == "." {
			prefix = ""
		} else {
			prefix += "/"
		}
	}

	entries, err := os.ReadDir(dirToRead)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var completions []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if filter != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(filter)) {
			continue
		}
		suffix := ""
		if e.IsDir() {
			suffix = "/"
		}
		completions = append(completions, colonPrefix+prefix+name+suffix)
	}
	return completions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

// runRemoteTransfer moves one file between this machine and a fleet member. The
// far campaign root is resolved by the remote's own camp, never computed here.
func runRemoteTransfer(ctx context.Context, cmd *cobra.Command, root string, src, dest transfer.Endpoint, force bool) error {
	remoteEnd, localSpec, pull := dest, src.Spec, false
	if src.IsRemote() {
		remoteEnd, localSpec, pull = src, dest.Spec, true
	}

	mf, err := machines.Load()
	if err != nil {
		return err
	}
	m, _, found := mf.Lookup(remoteEnd.Machine)
	if !found {
		return camperrors.New("unknown machine \"" + remoteEnd.Machine + "\"; add it to ~/.obey/machines.yaml")
	}
	// Same auth precondition as hop/switch: password-auth machines fail here
	// with an actionable message instead of a BatchMode transport failure.
	if err := remote.EnsureKeyAuth(m); err != nil {
		return err
	}

	// Cross-campaign local resolution (other-campaign:path, local:other:path →
	// Spec "other:path") matches the local transfer path. ResolveCampaignRelative
	// alone would treat the colon as a literal filename under the current root.
	localPath, err := transfer.ResolveCrossCampaignPath(ctx, root, localSpec)
	if err != nil {
		return camperrors.Wrap(err, "resolve local path")
	}
	if pull && transfer.IsDestDir(localPath) {
		localPath = filepath.Join(localPath, filepath.Base(remoteEnd.Path))
	}
	if !pull {
		if err := transfer.ValidatePathExists(localPath); err != nil {
			return camperrors.Wrap(err, "source")
		}
	} else {
		// Parity with local transfer: create missing parent dirs before the copy.
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return camperrors.Wrap(err, "create destination directory")
		}
	}

	if err := transfer.CopyRemote(ctx, transfer.CopyOptions{
		Machine:  m,
		Endpoint: remoteEnd,
		Local:    localPath,
		Pull:     pull,
		Force:    force,
	}); err != nil {
		if errors.Is(err, transfer.ErrDestinationExists) {
			// Phrase by where the destination lives — m.ID is always the far
			// machine, so a pull must not blame the remote for a local path.
			if pull {
				return camperrors.Newf("destination %q already exists (use --force to overwrite)", localPath)
			}
			return camperrors.Newf("destination exists on %s, not overwritten (use --force)", m.ID)
		}
		return camperrors.Wrapf(err, "run 'camp machine diagnose %s' to check reachability", m.ID)
	}

	_, err = fmt.Printf("Transferred %s -> %s\n", src.Spec, dest.Spec)
	return err
}
