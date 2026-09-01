package intent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intentcore "github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
)

// NoteFoldersJSONVersion is the schema_version for camp intent notes folders --json.
const NoteFoldersJSONVersion = "intent-note-folders/v1alpha1"

// NoteFoldersPayload is the --json contract for folder listing.
type NoteFoldersPayload struct {
	SchemaVersion string           `json:"schema_version"`
	GeneratedAt   time.Time        `json:"generated_at"`
	CampaignRoot  string           `json:"campaign_root"`
	Folders       []NoteFolderItem `json:"folders"`
}

// NoteFolderItem is one folder row in the JSON contract.
type NoteFolderItem struct {
	Status   string `json:"status"`
	Name     string `json:"name"`
	Depth    int    `json:"depth"`
	Reserved bool   `json:"reserved"`
	Count    int    `json:"count"`
}

var intentNotesCmd = &cobra.Command{
	Use:   "notes",
	Short: "Manage the note store (folders, moves, meetings)",
	Long: `Manage the camp note store under .campaign/intents/notes/.

Use "camp idea note" to capture a note. This command group manages folders
and placement of notes already in the store.

Examples:
  camp idea notes folders                 List note folders
  camp idea notes folders --json          Machine-readable folder list
  camp idea notes folders add reading     Create notes/reading/
  camp idea notes folders rm reading      Remove empty folder
  camp idea notes folders mv a b          Rename folder a → b
  camp idea notes mv <note-id> reading    Move a note into a folder`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	Cmd.AddCommand(intentNotesCmd)

	var foldersJSON bool
	foldersCmd := &cobra.Command{
		Use:   "folders",
		Short: "List note folders",
		Long: `List every note folder under .campaign/intents/notes/.

Order: notes root, reserved folders (meetings, archived), then user folders
alphabetically depth-first.

Examples:
  camp idea notes folders
  camp idea notes folders --json`,
	}
	foldersJSONRequested := func() bool { return intentJSONRequested(foldersCmd, &foldersJSON) }
	foldersCmd.Args = jsoncontract.Args(NoteFoldersJSONVersion, foldersJSONRequested, cobra.NoArgs)
	foldersCmd.RunE = jsoncontract.RunE(NoteFoldersJSONVersion, foldersJSONRequested, runNotesFoldersList)
	foldersCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(NoteFoldersJSONVersion, foldersJSONRequested))
	foldersCmd.Flags().BoolVar(&foldersJSON, "json", false, "emit a structured JSON result")
	intentNotesCmd.AddCommand(foldersCmd)

	foldersCmd.AddCommand(&cobra.Command{
		Use:   "add <folder>",
		Short: "Create a note folder",
		Long: `Create a note folder under notes/ (parents created as needed).

Folder names must be lowercase kebab-case. Reserved names (archived, meetings)
are rejected at the notes root. A .gitkeep is written so empty folders survive git.

Examples:
  camp idea notes folders add reading
  camp idea notes folders add reading/papers`,
		Args: cobra.ExactArgs(1),
		RunE: runNotesFoldersAdd,
	})

	foldersCmd.AddCommand(&cobra.Command{
		Use:   "rm <folder>",
		Short: "Remove an empty note folder",
		Long: `Remove a note folder. Refuses when the folder still contains notes or
child directories. Reserved folders cannot be removed.

Examples:
  camp idea notes folders rm reading/papers`,
		Args: cobra.ExactArgs(1),
		RunE: runNotesFoldersRm,
	})

	foldersCmd.AddCommand(&cobra.Command{
		Use:   "mv <from> <to>",
		Short: "Rename a note folder",
		Long: `Rename a note folder via directory move. Contained notes keep their
files; status is derived from location so frontmatter is not rewritten.

Examples:
  camp idea notes folders mv reading readings`,
		Args: cobra.ExactArgs(2),
		RunE: runNotesFoldersMv,
	})

	intentNotesCmd.AddCommand(&cobra.Command{
		Use:   "mv <note-id> <folder>",
		Short: "Move a note into a folder",
		Long: `Move a note into a folder under notes/. Use "" or "." for the notes root.

The destination folder must already exist (create it with folders add first).

Examples:
  camp idea notes mv nested-paper-20260101-000001 reading/papers
  camp idea notes mv nested-paper-20260101-000001 .`,
		Args: cobra.ExactArgs(2),
		RunE: runNotesMv,
	})
}

func loadNotesService(cmd *cobra.Command) (*intentcore.IntentService, string, error) {
	ctx := cmd.Context()
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, "", err
	}
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intentcore.NewIntentService(campaignRoot, resolver.Intents())
	if err := svc.EnsureDirectories(ctx); err != nil {
		return nil, "", camperrors.Wrap(err, "ensuring idea directories")
	}
	return svc, campaignRoot, nil
}

func runNotesFoldersList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	svc, campaignRoot, err := loadNotesService(cmd)
	if err != nil {
		return err
	}

	folders, err := svc.NoteFolders(ctx)
	if err != nil {
		return err
	}

	jsonOut, _ := cmd.Flags().GetBool("json")
	if intentJSONRequested(cmd, &jsonOut) {
		return outputNoteFoldersPayload(cmd.OutOrStdout(), campaignRoot, folders)
	}

	if len(folders) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "No note folders.")
		return camperrors.Wrap(err, "writing note folders")
	}
	for _, f := range folders {
		indent := strings.Repeat("  ", f.Depth)
		reserved := ""
		if f.Reserved {
			reserved = " [reserved]"
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s%s (%d)%s\n", indent, f.Name, f.Count, reserved); err != nil {
			return camperrors.Wrap(err, "writing note folders")
		}
	}
	return nil
}

func runNotesFoldersAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	svc, _, err := loadNotesService(cmd)
	if err != nil {
		return err
	}
	folder, err := svc.CreateNoteFolder(ctx, args[0])
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "✓ Note folder created: %s\n", folder.Status)
	return camperrors.Wrap(err, "writing note folder result")
}

func runNotesFoldersRm(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	svc, _, err := loadNotesService(cmd)
	if err != nil {
		return err
	}
	canonical, err := intentcore.NormalizeNoteFolderRel(args[0])
	if err != nil {
		return err
	}
	if err := svc.DeleteNoteFolder(ctx, canonical); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "✓ Note folder removed: %s\n", canonical)
	return camperrors.Wrap(err, "writing note folder result")
}

func runNotesFoldersMv(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	svc, _, err := loadNotesService(cmd)
	if err != nil {
		return err
	}
	from, err := intentcore.NormalizeNoteFolderRel(args[0])
	if err != nil {
		return err
	}
	to, err := intentcore.NormalizeNoteFolderRel(args[1])
	if err != nil {
		return err
	}
	if err := svc.RenameNoteFolder(ctx, from, to); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "✓ Note folder renamed: %s → %s\n", from, to)
	return camperrors.Wrap(err, "writing note folder result")
}

func runNotesMv(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	svc, _, err := loadNotesService(cmd)
	if err != nil {
		return err
	}
	folder := args[1]
	if folder == "." {
		folder = ""
	}
	note, err := svc.MoveNoteToFolder(ctx, args[0], folder)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "✓ Note moved: %s → %s\n", note.ID, note.Status)
	return camperrors.Wrap(err, "writing note move result")
}

func outputNoteFoldersPayload(w io.Writer, campaignRoot string, folders []intentcore.NoteFolder) error {
	// Always initialize as empty slice so JSON encodes [] not null.
	items := make([]NoteFolderItem, 0, len(folders))
	for _, f := range folders {
		items = append(items, NoteFolderItem{
			Status:   string(f.Status),
			Name:     f.Name,
			Depth:    f.Depth,
			Reserved: f.Reserved,
			Count:    f.Count,
		})
	}
	payload := NoteFoldersPayload{
		SchemaVersion: NoteFoldersJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		CampaignRoot:  campaignRoot,
		Folders:       items,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return camperrors.Wrap(err, "failed to marshal JSON")
	}
	return nil
}
