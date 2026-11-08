package leverage

import (
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"
	"path/filepath"
)

func resolveTargetDir(cmd *cobra.Command, args []string) (string, error) {
	dirFlag, _ := cmd.Flags().GetString("dir")

	target := dirFlag
	if target == "" && len(args) > 0 {
		target = args[0]
	}
	if target == "" {
		return "", nil
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return "", camperrors.Wrap(err, "resolving directory path")
	}
	return abs, nil
}

func runLeverageDir(cmd *cobra.Command, targetDir string) error {
	return runRepositoryReport(cmd, targetDir, "")
}
