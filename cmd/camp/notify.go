package main

import (
	"fmt"
	"io"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/notice"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// Every dismissible notice prints its own dismiss command, so this surface is
// never the only way to act on one. A user who wants the notice gone can copy
// the line they are already reading rather than going looking for a command
// they have not met yet.
var notifyCmd = &cobra.Command{
	Use:   "notify",
	Short: "Manage camp state notices",
	Long: `Manage the advisory notices camp surfaces on commands you already run.

Notices describe camp state you may not know is true, such as a declared
artifact root that has never synced. Each one carries its own dismiss command.

Dismissals are stored in .campaign/notices.yaml, which is committed: a
dismissal you make on one machine travels to your others, the same way the
artifact declarations it concerns do.`,
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
