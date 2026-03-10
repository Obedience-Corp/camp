package release

import "github.com/spf13/cobra"

// Register attaches release-profile-specific commands to the root command.
// camp currently has no user-facing dev-only commands, but this centralizes
// profile-specific wiring so it matches fest and is ready for future additions.
func Register(root *cobra.Command) {
	registerDev(root)
}
