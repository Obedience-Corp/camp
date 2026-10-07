package main

import (
	"fmt"
	"io"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/notice"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// Bare notify is where a person who met a notice on camp status goes to act on
// it: read the fix, copy it, or dismiss it in place, without carrying an id
// from one command to another. The subcommands keep the by-id path for
// scripts and agents, and every notice still prints its own dismiss command.
var notifyCmd = &cobra.Command{
	Use:   "notify",
	Short: "Review, dismiss, and restore camp state notices",
	Long: `Review the advisory notices camp surfaces on commands you already run.

Notices describe camp state you may not know is true, such as a declared
artifact root that has never synced. Run bare in a terminal to open the notice
browser: live notices first, then the ones you have dismissed. The selected
notice shows its fix command and id; d dismisses it, r restores a dismissed
one, y copies the fix, and ? lists every key.

Off a terminal, or with --plain, bare camp notify prints the same two lists.
--json emits them for scripts and agents. The dismiss, restore, and list
subcommands act on one id at a time.

Dismissals are stored in .campaign/notices.yaml, which is committed: a
dismissal you make on one machine travels to your others, the same way the
artifact declarations it concerns do.`,
	Example: `  camp notify                 # browse notices in a terminal; plain list otherwise
  camp notify --plain         # always print the plain list
  camp notify --json          # the same, for scripts and agents
  camp notify dismiss <id>    # dismiss one notice by id
  camp notify restore <id>    # show a dismissed notice again`,
	Args: jsoncontract.Args(NotifyJSONVersion, func() bool { return notifyOpts.json }, cobra.NoArgs),
	RunE: jsoncontract.RunE(NotifyJSONVersion, func() bool { return notifyOpts.json }, runNotify),
}

// NotifyJSONVersion is the schema of camp notify --json.
const NotifyJSONVersion = "notify/v1alpha1"

var notifyOpts struct {
	json  bool
	plain bool
}

var notifyDismissCmd = &cobra.Command{
	Use:   "dismiss <notice-id>",
	Short: "Stop showing a notice",
	Long: `Dismiss a notice by id.

Dismissal is per signature, not per kind. Dismissing the notice for one
artifact root does not silence a root you declare later: that one has its own
id and notifies on its own terms.`,
	Args: cobra.ExactArgs(1),
	RunE: runNotifyDismiss,
}

var notifyListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List dismissed notices",
	Args:    cobra.NoArgs,
	RunE:    runNotifyList,
}

func init() {
	notifyCmd.Flags().BoolVar(&notifyOpts.json, "json", false, "Emit live and dismissed notices as JSON")
	notifyCmd.Flags().BoolVar(&notifyOpts.plain, "plain", false, "Print the plain list even when stdout is a terminal")
	notifyCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(NotifyJSONVersion, func() bool { return notifyOpts.json }))
	notifyCmd.AddCommand(notifyDismissCmd)
	notifyCmd.AddCommand(notifyListCmd)
	rootCmd.AddCommand(notifyCmd)
}

func runNotifyDismiss(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp")
	}

	id := notice.CanonicalID(args[0])
	dismissals, err := notice.LoadDismissals(campRoot)
	if err != nil {
		return err
	}
	if !dismissals.Dismiss(id, time.Now()) {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s was already dismissed\n", ui.SuccessIcon(), id)
		return nil
	}
	if err := dismissals.Save(campRoot); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s Dismissed %s\n", ui.SuccessIcon(), id)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Recorded in %s. Undo: camp notify restore %s\n",
		notice.DismissalRelPath, id)
	return nil
}

var notifyRestoreCmd = &cobra.Command{
	Use:   "restore <notice-id>",
	Short: "Show a dismissed notice again",
	Args:  cobra.ExactArgs(1),
	RunE:  runNotifyRestore,
}

func init() {
	notifyCmd.AddCommand(notifyRestoreCmd)
}

func runNotifyRestore(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp")
	}

	id := notice.CanonicalID(args[0])
	dismissals, err := notice.LoadDismissals(campRoot)
	if err != nil {
		return err
	}
	if !dismissals.Restore(id) {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s is not dismissed\n", ui.SuccessIcon(), id)
		return nil
	}
	if err := dismissals.Save(campRoot); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s Restored %s\n", ui.SuccessIcon(), id)
	return nil
}

func runNotifyList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp")
	}

	dismissals, err := notice.LoadDismissals(campRoot)
	if err != nil {
		return err
	}
	if len(dismissals.Dismissed) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No dismissed notices")
		return nil
	}

	writeDismissedNotices(cmd.OutOrStdout(), dismissals.Describe(notice.Subjects(campRoot)))
	return nil
}

// writeDismissedNotices prints each dismissal with what it is about, so an id
// is never the only thing the user has to recognize it by.
func writeDismissedNotices(w io.Writer, dismissed []notice.Dismissed) {
	_, _ = fmt.Fprintf(w, "DISMISSED NOTICES\n")
	for _, d := range dismissed {
		subject := d.Subject
		if subject == "" && notice.HasSubject(d.ID) {
			subject = "(root no longer declared)"
		}
		if subject == "" {
			_, _ = fmt.Fprintf(w, "  %s\n", d.ID)
		} else {
			_, _ = fmt.Fprintf(w, "  %s  %s\n", d.ID, subject)
		}
		summary := d.Summary
		if summary == "" {
			summary = "a notice this version of Camp does not raise"
		}
		_, _ = fmt.Fprintf(w, "    %s\n", ui.Dim(summary+", dismissed "+d.At))
	}
	_, _ = fmt.Fprintf(w, "\nRestore one: camp notify restore <id>\n")
}
