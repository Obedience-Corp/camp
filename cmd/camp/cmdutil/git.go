package cmdutil

import (
	"context"
	"fmt"
	"io"
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

// ShowStagedSummaryTo writes the staged summary to an explicit writer, so a
// caller emitting machine output can keep it off stdout.
func ShowStagedSummaryTo(w io.Writer, ctx context.Context, repoPath string) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--cached", "--stat")
	output, err := cmd.Output()
	if err != nil {
		return
	}
	if len(output) > 0 {
		_, _ = fmt.Fprintln(w, "\nChanges to be committed:")
		_, _ = fmt.Fprint(w, string(output))
	}
}
