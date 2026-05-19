//go:build dev

package release

import (
	flowcmd "github.com/Obedience-Corp/camp/internal/commands/flow"
	"github.com/spf13/cobra"
)

func registerDev(root *cobra.Command) {
	flowCmd := flowcmd.NewFlowCommand()
	flowCmd.GroupID = "planning"
	MarkDevOnly(flowCmd)
	root.AddCommand(flowCmd)
}
