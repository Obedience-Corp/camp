package workflow

import "github.com/spf13/cobra"

// NewWorkflowCommand creates the camp workflow command.
func NewWorkflowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Manage workflow collections",
		Long: `Manage workflow collections.

A workflow collection is a campaign directory under workflow/<type>/ with
navigation config and workitem type support.`,
	}

	cmd.AddCommand(newCreateCommand())
	cmd.AddCommand(newListCommand())
	cmd.AddCommand(newShowCommand())
	cmd.AddCommand(newShortcutCommand())
	cmd.AddCommand(newDoctorCommand())
	cmd.AddCommand(newSyncCommand())
	return cmd
}
