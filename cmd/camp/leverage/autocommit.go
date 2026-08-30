package leverage

import (
	"context"
	"fmt"
	"io"

	"github.com/Obedience-Corp/camp/internal/config"
	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/spf13/cobra"
)

const noCommitFlag = "no-commit"

func addNoCommitFlag(cmd *cobra.Command) {
	cmd.Flags().Bool(noCommitFlag, false,
		"skip the automatic commit of "+intleverage.DataDir+" data")
}

// autocommitInput is one command's request to record what it just wrote.
type autocommitInput struct {
	root    string
	cfg     *intleverage.LeverageConfig
	subject string
	report  string

	// ignoreConfigOptOut commits even when cfg turns autocommit off. Only
	// `camp leverage config --autocommit` sets it, because the write it is
	// recording is the opt-out itself: honoring the new setting here would
	// leave the setting change sitting uncommitted with nothing said about it.
	// --no-commit still wins.
	ignoreConfigOptOut bool
}

// autocommitLeverageData commits the leverage data directory and reports the
// outcome on out in a single line.
//
// Nothing here can fail the command that called it. The files are already on
// disk, so a repository camp cannot commit into is a thing to say, not a
// reason to turn a successful scoring run into an error.
func autocommitLeverageData(ctx context.Context, cmd *cobra.Command, out io.Writer, in autocommitInput) {
	if reason, ok := autocommitDisabled(cmd, in); ok {
		reportAutocommitSkip(cmd, reason)
		return
	}

	req := intleverage.CommitRequest{
		CampaignRoot: in.root,
		Subject:      in.subject,
		Report:       in.report,
	}
	if cfg, err := config.LoadCampaignConfig(ctx, in.root); err == nil {
		req.CampaignName = cfg.Name
		req.CampaignID = cfg.ID
	}

	res, err := intleverage.Autocommit(ctx, req, nil)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not commit leverage data: %v\n", err)
		return
	}
	if res.Status != intleverage.CommitCreated {
		reportAutocommitSkip(cmd, res.Reason)
		return
	}
	_, _ = fmt.Fprintf(out, "committed leverage data: %s (%s)\n", in.subject, shortSHA(res.Hash))
}

// autocommitDisabled reports the user's opt-out, if any. The flag is checked
// before the config so an explicit --no-commit wins over a config that enables
// autocommit.
func autocommitDisabled(cmd *cobra.Command, in autocommitInput) (string, bool) {
	if noCommit, err := cmd.Flags().GetBool(noCommitFlag); err == nil && noCommit {
		return "--" + noCommitFlag, true
	}
	if !in.ignoreConfigOptOut && !in.cfg.AutocommitEnabled() {
		return "autocommit is disabled in " + intleverage.DataDir + "/config.json", true
	}
	return "", false
}

// reportAutocommitSkip explains a skip only under --verbose. A skip means the
// commit camp would have made adds nothing, and a line about it on every run
// would be noise on the path the user actually asked about.
func reportAutocommitSkip(cmd *cobra.Command, reason string) {
	if reason == "" {
		return
	}
	if verbose, err := cmd.Flags().GetBool("verbose"); err != nil || !verbose {
		return
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[verbose] leverage autocommit skipped: %s\n", reason)
}

// autocommitWriter keeps the report line off stdout while --json owns it.
func autocommitWriter(cmd *cobra.Command) io.Writer {
	if leverageJSON {
		return cmd.ErrOrStderr()
	}
	return cmd.OutOrStdout()
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
