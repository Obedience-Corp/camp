//go:build dev

package release

import (
	flowcmd "github.com/Obedience-Corp/camp/internal/commands/flow"
	freshcmd "github.com/Obedience-Corp/camp/internal/commands/fresh"
	"github.com/spf13/cobra"
)

func registerDev(root *cobra.Command) {
	flowCmd := flowcmd.NewFlowCommand()
	flowCmd.GroupID = "planning"
	MarkDevOnly(flowCmd)
	root.AddCommand(flowCmd)

	freshCmd := freshcmd.NewFreshCommand()
	freshCmd.GroupID = "git"
	MarkDevOnly(freshCmd)
	root.AddCommand(freshCmd)
}
