package cmdutil

import (
	"context"
	"fmt"
	"os/exec"
)

// ShowStagedSummary prints the staged diffstat for the target repository.
func ShowStagedSummary(ctx context.Context, repoPath string) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--cached", "--stat")
	output, err := cmd.Output()
	if err != nil {
		return
	}

	if len(output) > 0 {
		fmt.Println("\nChanges to be committed:")
		fmt.Print(string(output))
		fmt.Println()
	}
}
