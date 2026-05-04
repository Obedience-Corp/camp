package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pins"
	"github.com/spf13/cobra"
)

var pinsCmd = &cobra.Command{
	Use:   "pins",
	Short: "List all pinned directories",
	Long:  `List all saved pins. Use 'camp pin' to add and 'camp unpin' to remove.`,
	RunE:  runPinsList,
}

var pinCmd = &cobra.Command{
	Use:   "pin <name> [path]",
	Short: "Pin a directory",
	Long: `Pin a directory for quick navigation with 'camp go <name>' or 'cgo <name>'.

If path is omitted, the current working directory is used.`,
	Example: `  camp pin code                        # Pin current directory as "code"
  camp pin design workflow/design/my-project
  camp go code                         # Jump to a pin by name
  cgo design                           # Shell jump to a pin`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runPin,
}

var unpinCmd = &cobra.Command{
	Use:   "unpin [name]",
	Short: "Remove a saved pin",
	Long: `Remove a saved pin by name.

Without arguments, detects and unpins the current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runUnpin,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		store, _, err := loadPinStore(cmd)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		return store.Names(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	rootCmd.AddCommand(pinsCmd)
	rootCmd.AddCommand(pinCmd)
	rootCmd.AddCommand(unpinCmd)

	pinsCmd.GroupID = "navigation"
	pinCmd.GroupID = "navigation"
	unpinCmd.GroupID = "navigation"
}

// loadPinStore loads the pin store from .campaign/settings/pins.json.
// Returns the store and the campaign root path.
func loadPinStore(cmd *cobra.Command) (*pins.Store, string, error) {
	ctx := cmd.Context()
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, "", err
	}
	pins.MigrateLegacyStore(
		root,
		filepath.Join(root, campaign.CampaignDir, "pins.json"),
		config.PinsConfigPath(root),
	)
	storePath := config.PinsConfigPath(root)
	store := pins.NewStore(storePath)
	if err := store.Load(); err != nil {
		return nil, "", err
	}
	return store, root, nil
}

func runPinsList(cmd *cobra.Command, args []string) error {
	store, _, err := loadPinStore(cmd)
	if err != nil {
		return err
	}

	pinList := store.List()
	if len(pinList) == 0 {
		fmt.Println("No pins saved. Use 'camp pin <name>' to pin a directory.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "NAME\tKIND\tPATH\tCREATED"); err != nil {
		return err
	}
	for _, p := range pinList {
		kind := "in-tree"
		path := p.Path
		if p.IsAttachment() {
			kind = "attachment"
			path = p.AbsPath
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name, kind, path, p.CreatedAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return w.Flush()
}

func runPin(cmd *cobra.Command, args []string) error {
	name := args[0]

	var dir string
	if len(args) > 1 {
		dir = args[1]
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return camperrors.Wrap(err, "get working directory")
		}
		dir = cwd
	}

	// Resolve to canonical absolute path (follows symlinks so the
	// comparison with the symlink-resolved campaign root is consistent)
	absPath, err := filepath.Abs(dir)
	if err != nil {
		return camperrors.Wrap(err, "resolve path")
	}
	absPath, err = filepath.EvalSymlinks(absPath)
	if err != nil {
		return camperrors.Wrapf(err, "resolve symlinks for %q", dir)
	}

	// Validate path exists and is a directory
	info, err := os.Stat(absPath)
	if err != nil {
		return camperrors.Wrapf(err, "path %q does not exist", absPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %q is not a directory", absPath)
	}

	store, campaignRoot, err := loadPinStore(cmd)
	if err != nil {
		return err
	}

	pin, display, err := buildPinForPath(absPath, campaignRoot)
	if err != nil {
		return err
	}
	pin.Name = name

	result := store.TogglePin(pin)
	if err := store.Save(); err != nil {
		return err
	}

	switch result {
	case pins.Pinned:
		fmt.Printf("Pinned %q → %s\n", name, display)
	case pins.Unpinned:
		fmt.Printf("Unpinned %q (was already pinned to same path)\n", name)
	case pins.Updated:
		fmt.Printf("Updated pin %q → %s\n", name, display)
	}
	return nil
}

// buildPinForPath returns a Pin record (without Name) and a display string for
// the given absolute path. In-tree paths get a campaign-relative Path. Paths
// outside the campaign tree are accepted only when they resolve through a
// .camp attachment marker that points at the active campaign; those become
// AbsPath pins.
func buildPinForPath(absPath, campaignRoot string) (pins.Pin, string, error) {
	relPath, err := filepath.Rel(campaignRoot, absPath)
	if err != nil {
		return pins.Pin{}, "", camperrors.Wrapf(err, "compute relative path for %q", absPath)
	}
	if relPath != ".." && !strings.HasPrefix(relPath, "../") {
		return pins.Pin{Path: relPath}, relPath, nil
	}

	// Outside the campaign tree: only allow if a marker resolves to this
	// campaign root.
	if !markerResolvesToCampaign(absPath, campaignRoot) {
		return pins.Pin{}, "", camperrors.Wrapf(fmt.Errorf("outside campaign root"),
			"pin path %q (run 'camp attach %s' first to bind this directory to the campaign)",
			absPath, absPath)
	}
	return pins.Pin{AbsPath: absPath}, absPath + " (attachment)", nil
}

// markerResolvesToCampaign returns true when walking up from path reaches a
// campaign root that matches campaignRoot. This succeeds for paths under an
// attachment marker pointing at the active campaign.
func markerResolvesToCampaign(path, campaignRoot string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), campaign.DefaultDetectTimeout)
	defer cancel()

	root, err := campaign.Detect(ctx, path)
	if err != nil {
		return false
	}
	resolvedRoot, err := filepath.EvalSymlinks(campaignRoot)
	if err != nil {
		resolvedRoot = campaignRoot
	}
	return root == resolvedRoot
}

func runUnpin(cmd *cobra.Command, args []string) error {
	store, campaignRoot, err := loadPinStore(cmd)
	if err != nil {
		return err
	}

	var name string
	if len(args) == 1 {
		name = args[0]
	} else {
		// No args — detect pin from current directory
		cwd, err := os.Getwd()
		if err != nil {
			return camperrors.Wrap(err, "get working directory")
		}
		cwd, err = filepath.EvalSymlinks(cwd)
		if err != nil {
			return camperrors.Wrap(err, "resolve symlinks for working directory")
		}
		pin, ok := findPinForCwd(store, cwd, campaignRoot)
		if !ok {
			return fmt.Errorf("directory not pinned: %s", cwd)
		}
		name = pin.Name
	}

	if err := store.Remove(name); err != nil {
		return err
	}
	if err := store.Save(); err != nil {
		return err
	}

	fmt.Printf("Unpinned %q\n", name)
	return nil
}

// findPinForCwd returns the pin matching cwd, looking up either by
// campaign-relative Path (in-tree) or by AbsPath (attachment).
func findPinForCwd(store *pins.Store, cwd, campaignRoot string) (pins.Pin, bool) {
	if pin, ok := store.FindByPath(cwd); ok {
		return pin, true
	}
	for _, p := range store.List() {
		if p.AbsPath != "" && p.AbsPath == cwd {
			return p, true
		}
	}
	if relCwd, err := filepath.Rel(campaignRoot, cwd); err == nil {
		if pin, ok := store.FindByPath(relCwd); ok {
			return pin, true
		}
	}
	return pins.Pin{}, false
}

// pinNotFoundError returns an error with suggestions for similar pin names.
func pinNotFoundError(name string, store *pins.Store) error {
	names := store.Names()
	if len(names) == 0 {
		return fmt.Errorf("pin %q not found (no pins saved — use 'camp pin' to create one)", name)
	}
	return fmt.Errorf("pin %q not found (available: %s)", name, strings.Join(names, ", "))
}
