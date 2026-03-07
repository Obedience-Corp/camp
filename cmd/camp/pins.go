package main

import (
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/pins"
	"github.com/spf13/cobra"
)

var pinsCmd = &cobra.Command{
	Use:     "pins",
	Short:   "List all pinned directories",
	Long:    `List all pinned directory bookmarks. Use 'camp pin' to add and 'camp unpin' to remove.`,
	Aliases: []string{"bookmarks"},
	RunE:    runPinsList,
}

var pinCmd = &cobra.Command{
	Use:   "pin <name> [path]",
	Short: "Bookmark a directory",
	Long: `Bookmark a directory for quick navigation with 'camp jump'.

If path is omitted, the current working directory is used.`,
	Example: `  camp pin myspot           # Pin current directory as "myspot"
  camp pin docs /path/to/docs  # Pin a specific path`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runPin,
}

var unpinCmd = &cobra.Command{
	Use:   "unpin [name]",
	Short: "Remove a directory bookmark",
	Long: `Remove a pinned directory bookmark by name.

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
	migratePinsIfNeeded(root)
	storePath := config.PinsConfigPath(root)
	store := pins.NewStore(storePath)
	if err := store.Load(); err != nil {
		return nil, "", err
	}
	return store, root, nil
}

// migratePinsIfNeeded moves pins.json from .campaign/ to .campaign/settings/ if needed,
// and converts any absolute paths to campaign-root-relative paths.
func migratePinsIfNeeded(root string) {
	oldPath := filepath.Join(root, campaign.CampaignDir, "pins.json")
	newPath := config.PinsConfigPath(root)
	if _, err := os.Stat(oldPath); err == nil {
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			_ = os.MkdirAll(filepath.Dir(newPath), 0755)
			_ = os.Rename(oldPath, newPath)
		}
	}

	// Migrate absolute paths to relative
	store := pins.NewStore(newPath)
	if err := store.Load(); err != nil {
		return
	}
	if store.MigrateAbsoluteToRelative(root) {
		_ = store.Save()
	}
}

func runPinsList(cmd *cobra.Command, args []string) error {
	store, _, err := loadPinStore(cmd)
	if err != nil {
		return err
	}

	pinList := store.List()
	if len(pinList) == 0 {
		fmt.Println("No pins saved. Use 'camp pin <name>' to bookmark a directory.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "NAME\tPATH\tCREATED\n")
	for _, p := range pinList {
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, p.Path, p.CreatedAt.Format(time.RFC3339))
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

	// Resolve to absolute path
	absPath, err := filepath.Abs(dir)
	if err != nil {
		return camperrors.Wrap(err, "resolve path")
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

	// Store path relative to campaign root for portability
	relPath, err := filepath.Rel(campaignRoot, absPath)
	if err != nil {
		return camperrors.Wrapf(err, "compute relative path for %q", absPath)
	}
	if relPath == ".." || strings.HasPrefix(relPath, "../") {
		return camperrors.Wrapf(fmt.Errorf("outside campaign root"), "pin path %q", absPath)
	}

	result := store.Toggle(name, relPath)
	if err := store.Save(); err != nil {
		return err
	}

	switch result {
	case pins.Pinned:
		fmt.Printf("Pinned %q → %s\n", name, relPath)
	case pins.Unpinned:
		fmt.Printf("Unpinned %q (was already pinned to same path)\n", name)
	case pins.Updated:
		fmt.Printf("Updated pin %q → %s\n", name, relPath)
	}
	return nil
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
		relCwd, err := filepath.Rel(campaignRoot, cwd)
		if err != nil {
			return camperrors.Wrap(err, "compute relative path")
		}
		pin, ok := store.FindByPath(relCwd)
		if !ok {
			return fmt.Errorf("directory not pinned: %s", relCwd)
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

// pinNotFoundError returns an error with suggestions for similar pin names.
func pinNotFoundError(name string, store *pins.Store) error {
	names := store.Names()
	if len(names) == 0 {
		return fmt.Errorf("pin %q not found (no pins saved — use 'camp pin' to create one)", name)
	}
	return fmt.Errorf("pin %q not found (available: %s)", name, strings.Join(names, ", "))
}
