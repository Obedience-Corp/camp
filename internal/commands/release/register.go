package release

import (
	freshcmd "github.com/Obedience-Corp/camp/internal/commands/fresh"
	"github.com/spf13/cobra"
)

// Register attaches commands to the root command.
// Commands available in all builds are registered here directly.
// Dev-only commands are registered via registerDev (register_dev.go).
func Register(root *cobra.Command) {
	freshCmd := freshcmd.NewFreshCommand()
	freshCmd.GroupID = "git"
	root.AddCommand(freshCmd)

	registerDev(root)
}
