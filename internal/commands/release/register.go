package release

import "github.com/spf13/cobra"

// Register attaches release-profile-specific commands to the root command.
// In dev builds, registerDev (from register_dev.go) wires up the flow and fresh
// commands which live in internal/commands/flow and internal/commands/fresh.
func Register(root *cobra.Command) {
	registerDev(root)
}
