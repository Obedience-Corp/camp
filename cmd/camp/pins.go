package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/pins"
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
	Use:   "unpin <name>",
	Short: "Remove a directory bookmark",
	Long:  `Remove a pinned directory bookmark by name.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runUnpin,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		store, err := loadPinStore(cmd)
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

// loadPinStore loads the pin store from .campaign/pins.json.
func loadPinStore(cmd *cobra.Command) (*pins.Store, error) {
	ctx := cmd.Context()
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, err
	}
	storePath := filepath.Join(root, campaign.CampaignDir, "pins.json")
	store := pins.NewStore(storePath)
	if err := store.Load(); err != nil {
		return nil, err
	}
	return store, nil
}

func runPinsList(cmd *cobra.Command, args []string) error {
	store, err := loadPinStore(cmd)
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
			return fmt.Errorf("get working directory: %w", err)
		}
		dir = cwd
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Validate path exists and is a directory
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("path %q does not exist: %w", absPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %q is not a directory", absPath)
	}

	store, err := loadPinStore(cmd)
	if err != nil {
		return err
	}

	if err := store.Add(name, absPath); err != nil {
		return err
	}
	if err := store.Save(); err != nil {
		return err
	}

	fmt.Printf("Pinned %q → %s\n", name, absPath)
	return nil
}

func runUnpin(cmd *cobra.Command, args []string) error {
	name := args[0]

	store, err := loadPinStore(cmd)
	if err != nil {
		return err
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
