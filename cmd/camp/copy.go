package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/transfer"
	"github.com/spf13/cobra"
)

var copyCmd = &cobra.Command{
	Use:   "copy <src> <dest>",
	Short: "Copy a file or directory within the campaign",
	Long: `Copy a file or directory within the current campaign.

Paths are resolved relative to the current directory, matching standard
'cp' behavior and tab completion.

Use @ prefix for campaign shortcuts (e.g., @p/fest, @f/active/).
Available shortcuts are defined in campaign config.

If the destination is an existing directory or ends with '/', the source
is placed inside it with the same basename. Directories are copied
recursively.`,
	Example: `  camp copy myfile.md ../docs/
  camp cp @f/active/my-fest/OVERVIEW.md @d/
  camp cp @w/design/active/ @w/explore/backup/`,
	Aliases:           []string{"cp"},
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeTransferArgs,
	RunE:              runCopy,
}

func init() {
	rootCmd.AddCommand(copyCmd)
	copyCmd.GroupID = "campaign"
	copyCmd.Flags().BoolP("force", "f", false, "Overwrite destination without prompting")
}

func runCopy(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	force, _ := cmd.Flags().GetBool("force")

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	// Load campaign config for @ shortcut resolution
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return err
	}
	shortcuts := buildShortcutsMap(cfg)

	// Resolve paths: @ prefix -> campaign shortcuts, otherwise -> cwd-relative
	src, err := resolveTransferArg(root, args[0], shortcuts)
	if err != nil {
		return camperrors.Wrap(err, "source")
	}
	dest, err := resolveTransferArg(root, args[1], shortcuts)
	if err != nil {
		return camperrors.Wrap(err, "destination")
	}

	if err := transfer.ValidatePathExists(src); err != nil {
		return camperrors.Wrap(err, "source")
	}

	// Stat source (needed for same-file guard, dest-under-src guard, and the copy dispatch below).
	srcInfo, err := os.Stat(src)
	if err != nil {
		return camperrors.Wrap(err, "stat source")
	}

	// If dest is a directory or ends with /, place source inside it
	if transfer.IsDestDir(dest) || transfer.IsDestDir(args[1]) {
		dest = filepath.Join(dest, filepath.Base(src))
	}

	// Same-file check: stat the resolved destination. If it exists and refers
	// to the same inode as src, refuse before opening with O_TRUNC.
	if destStatForSame, err := os.Stat(dest); err == nil {
		if os.SameFile(srcInfo, destStatForSame) {
			return camperrors.Wrapf(camperrors.ErrInvalidInput, "source and destination are the same file: %s", dest)
		}
	}

	if srcInfo.IsDir() {
		// Resolve both paths to their canonical forms before comparing.
		// On macOS, /var is a symlink to /private/var; without EvalSymlinks
		// a prefix check would silently fail and allow the recursion.
		resolvedSrc, err := filepath.EvalSymlinks(src)
		if err != nil {
			return camperrors.Wrap(err, "resolve source path")
		}
		resolvedDest, err := resolvePathThroughExistingAncestor(dest)
		if err != nil {
			return camperrors.Wrap(err, "resolve destination path")
		}
		// Guard: dest must not be inside src. Use separator-guarded prefix check
		// so that /foo/bar does not falsely match /foo/barsuffix.
		srcWithSep := resolvedSrc + string(filepath.Separator)
		if resolvedDest == resolvedSrc || strings.HasPrefix(resolvedDest, srcWithSep) {
			return camperrors.Wrapf(camperrors.ErrInvalidInput, "cannot copy a directory into itself: %s is inside %s", dest, src)
		}
	}

	if !force {
		if _, err := os.Stat(dest); err == nil {
			return camperrors.Wrapf(camperrors.ErrAlreadyExists, "destination %q already exists", dest)
		}
	}

	if srcInfo.IsDir() {
		if err := transfer.CopyDir(src, dest); err != nil {
			return camperrors.Wrap(err, "copy directory")
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return camperrors.Wrap(err, "create destination directory")
		}
		if err := transfer.CopyFile(src, dest); err != nil {
			return camperrors.Wrap(err, "copy file")
		}
	}

	srcRel, _ := filepath.Rel(root, src)
	destRel, _ := filepath.Rel(root, dest)
	fmt.Printf("Copied %s → %s\n", srcRel, destRel)
	return nil
}

func resolvePathThroughExistingAncestor(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}

	clean := filepath.Clean(path)
	current := clean
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			rel, relErr := filepath.Rel(current, clean)
			if relErr != nil {
				return "", relErr
			}
			return filepath.Join(resolved, rel), nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return clean, nil
		}
		current = parent
	}
}
