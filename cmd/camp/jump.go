package main

import (
	"fmt"

	"github.com/obediencecorp/camp/internal/pins"
	"github.com/spf13/cobra"
)

var jumpCmd = &cobra.Command{
	Use:   "jump <pin>",
	Short: "Navigate to a pinned directory",
	Long: `Navigate to a pinned directory bookmark by name.

Use with the cgo shell function for instant navigation:
  cgo jump myspot    # cd to the pinned directory

The --print flag outputs just the path for shell integration:
  cd "$(camp jump myspot --print)"`,
	Example: `  camp jump myspot        # Jump to pinned directory
  camp jump docs --print  # Print path only`,
	Aliases: []string{"j"},
	Args:    cobra.ExactArgs(1),
	RunE:    runJump,
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
	rootCmd.AddCommand(jumpCmd)
	jumpCmd.GroupID = "navigation"
	jumpCmd.Flags().Bool("print", false, "Print path only (for shell integration)")
}

func runJump(cmd *cobra.Command, args []string) error {
	printOnly, _ := cmd.Flags().GetBool("print")
	name := args[0]

	store, err := loadPinStore(cmd)
	if err != nil {
		return err
	}

	pin, ok := store.Get(name)
	if !ok {
		return pinNotFoundError(name, store)
	}

	if printOnly {
		fmt.Println(pin.Path)
	} else {
		fmt.Printf("cd %s\n", pin.Path)
	}
	return nil
}

// pinNotFoundError returns an error with suggestions for similar pin names.
func pinNotFoundError(name string, store *pins.Store) error {
	names := store.Names()
	if len(names) == 0 {
		return fmt.Errorf("pin %q not found (no pins saved — use 'camp pin' to create one)", name)
	}
	return fmt.Errorf("pin %q not found (available: %s)", name, joinNames(names))
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	result := names[0]
	for i := 1; i < len(names); i++ {
		result += ", " + names[i]
	}
	return result
}
