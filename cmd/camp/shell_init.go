package main

import (
	"fmt"
	"strings"

	"github.com/obediencecorp/camp/internal/shell"
	"github.com/spf13/cobra"
)

var shellInitCmd = &cobra.Command{
	Use:   "shell-init <shell>",
	Short: "Output shell initialization code",
	Long: `Output shell initialization code for your shell config.

Add to your shell config:
  zsh:  eval "$(camp shell-init zsh)"
  bash: eval "$(camp shell-init bash)"
  fish: camp shell-init fish | source

This provides:
  - cgo function for navigation
  - Tab completion for camp commands
  - Category shortcuts (p, c, f, etc.)

The cgo function enables quick navigation:
  cgo                 Interactive picker or jump to campaign root
  cgo p               Jump to projects/
  cgo p api           Fuzzy find "api" in projects/
  cgo -c p ls         Run "ls" in projects/ directory`,
	Example: `  # Add to ~/.zshrc
  eval "$(camp shell-init zsh)"

  # Add to ~/.bashrc
  eval "$(camp shell-init bash)"

  # Add to ~/.config/fish/config.fish
  camp shell-init fish | source`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: shell.SupportedShells,
	RunE:      runShellInit,
}

func init() {
	rootCmd.AddCommand(shellInitCmd)
}

func runShellInit(cmd *cobra.Command, args []string) error {
	shellType := strings.ToLower(args[0])

	if !shell.IsSupported(shellType) {
		return fmt.Errorf("unsupported shell: %s\nSupported: %s",
			shellType, strings.Join(shell.SupportedShells, ", "))
	}

	output, err := shell.Generate(shellType)
	if err != nil {
		return err
	}

	fmt.Print(output)
	return nil
}
