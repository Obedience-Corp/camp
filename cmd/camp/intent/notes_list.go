package intent

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intentcore "github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/ui"
)

func newIntentNotesListCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List notes across folders",
		Long: `List notes in the note store, newest first by creation time.

Notes in the notes root and every folder, including meetings, are listed.
Archived notes are listed only with --folder archived. --folder matches one
folder exactly, without its subfolders; use "." for the notes root.

"camp idea list --status notes" returns the same notes.

Examples:
  camp idea notes list                          All notes except archived
  camp idea notes list --folder reading         Notes directly in notes/reading/
  camp idea notes list --folder .               Notes in the notes root only
  camp idea notes list --json                   Machine-readable note list`,
	}
	jsonRequested := func() bool { return intentJSONRequested(cmd, &jsonOut) }
	cmd.Args = jsoncontract.Args(IntentJSONVersion, jsonRequested, cobra.NoArgs)
	cmd.RunE = jsoncontract.RunE(IntentJSONVersion, jsonRequested, runIntentNotesList)
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(IntentJSONVersion, jsonRequested))

	flags := cmd.Flags()
	flags.BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	flags.String("folder", "", `Only list notes directly in this folder under notes/ ("." for the root)`)
	return cmd
}

func init() {
	intentNotesCmd.AddCommand(newIntentNotesListCommand())
}

func runIntentNotesList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	jsonOut, _ := cmd.Flags().GetBool("json")
	folder, _ := cmd.Flags().GetString("folder")
	folderSet := cmd.Flags().Changed("folder")

	svc, campaignRoot, err := loadIntentReader(ctx)
	if err != nil {
		return err
	}

	if folderSet {
		folder, err = svc.ResolveNoteFolder(ctx, folder)
		if err != nil {
			return jsoncontract.WithHint(err, "run 'camp idea notes folders' to see the note folders")
		}
	}

	entries, err := svc.ListNoteEntries(ctx, folderSet)
	if err != nil {
		return camperrors.Wrap(err, "listing notes")
	}
	if folderSet {
		entries = filterNoteEntriesByFolder(entries, folder)
	}
	sortNoteEntriesNewestFirst(entries)

	if jsonOut {
		notes, noteFolders := splitNoteEntries(entries)
		return outputIntentListPayload(cmd.OutOrStdout(), campaignRoot, notes, noteFolders)
	}
	return outputNotesTable(cmd.OutOrStdout(), entries)
}

// loadIntentReader opens the intent service for a read-only command. Unlike
// loadNotesService it does not create missing directories, and it resolves the
// camp root so JSON paths join against campaign_root.
func loadIntentReader(ctx context.Context) (*intentcore.IntentService, string, error) {
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, "", camperrors.Wrap(err, "not in a camp directory")
	}
	campaignRoot, err = pathutil.ResolveRoot(campaignRoot)
	if err != nil {
		return nil, "", camperrors.Wrap(err, "resolving camp root")
	}
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	return intentcore.NewIntentService(campaignRoot, resolver.Intents()), campaignRoot, nil
}

func filterNoteEntriesByFolder(entries []intentcore.NoteEntry, folder string) []intentcore.NoteEntry {
	return slices.DeleteFunc(entries, func(e intentcore.NoteEntry) bool {
		return e.Folder != folder
	})
}

func sortNoteEntriesNewestFirst(entries []intentcore.NoteEntry) {
	slices.SortFunc(entries, func(a, b intentcore.NoteEntry) int {
		if c := b.Note.CreatedAt.Compare(a.Note.CreatedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.Note.ID, b.Note.ID)
	})
}

// splitNoteEntries returns the notes in order plus the folder lookup that
// outputIntentListPayload uses to render them as note items.
func splitNoteEntries(entries []intentcore.NoteEntry) ([]*intentcore.Intent, map[*intentcore.Intent]string) {
	notes := make([]*intentcore.Intent, 0, len(entries))
	folders := make(map[*intentcore.Intent]string, len(entries))
	for _, e := range entries {
		notes = append(notes, e.Note)
		folders[e.Note] = e.Folder
	}
	return notes, folders
}

func outputNotesTable(w io.Writer, entries []intentcore.NoteEntry) error {
	if len(entries) == 0 {
		_, err := fmt.Fprintln(w, "No notes found.")
		return camperrors.Wrap(err, "writing notes")
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{
			truncate(e.Note.Title, 50),
			dashIfEmpty(e.Folder),
			dashIfEmpty(truncate(strings.Join(e.Note.Tags, ", "), 30)),
			dashIfEmpty(e.Note.Author),
			formatTimestamp(e.Note.CreatedAt),
		})
	}

	t := table.New().
		Border(lipgloss.HiddenBorder()).
		Headers("TITLE", "FOLDER", "TAGS", "AUTHOR", "CREATED").
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return lipgloss.NewStyle()
		})

	_, err := fmt.Fprintf(w, "%s\n\n%d note(s)\n", t, len(entries))
	return camperrors.Wrap(err, "writing notes")
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
