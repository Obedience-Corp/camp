package intent

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	wkcmd "github.com/Obedience-Corp/camp/internal/commands/workitem"
	"github.com/Obedience-Corp/camp/internal/concept"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
	navtui "github.com/Obedience-Corp/camp/internal/nav/tui"
	"github.com/Obedience-Corp/camp/internal/paths"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// noteRefPrefix is the NT- tag prefix for note commits, mirroring
// wkitem.RefPrefix ("WI-") for workitems.
const noteRefPrefix = "NT-"

// noteRef derives the NT-<6 hex> commit tag ref for a note from its
// frontmatter id, reusing the same sha256-prefix scheme workitem refs use
// (internal/workitem/ref.go) via wkitem.DeriveWithPrefix.
func noteRef(id string) string {
	return wkitem.DeriveWithPrefix(noteRefPrefix, id)
}

var intentNoteCmd = &cobra.Command{
	Use:   "note [text]",
	Short: "Capture a quick note",
	Long: `Capture a freeform note. Notes are a separate category from intents: they
are stored in .campaign/intents/notes/ and do not flow through the
inbox → ready → active lifecycle. A note carries no type or concept; tags
organize them.

Fast capture skips the TUI. Interactive capture uses the same title/body/tag
flow as intent add, but skips the type wheel and concept picker.

Examples:
  camp intent note "check the daemon socket path"   Capture a note immediately
  camp intent note "follow up" --body "details..."  Note with a longer body
  echo "body" | camp intent note "idea" --body-file -
  camp intent note                                  Note TUI (title + body)`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIntentNote,
}

func init() {
	Cmd.AddCommand(intentNoteCmd)

	flags := intentNoteCmd.Flags()
	flags.Bool("no-commit", false, "Don't create a git commit")
	flags.String("body", "", "Set note body as a literal string")
	flags.String("body-file", "", "Read note body from file (- for stdin, 10 MiB cap)")
	flags.String("author", "", "Override the default author attribution")
	flags.StringArrayP("tag", "t", nil, "Add a tag (repeatable)")
}

func runIntentNote(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	noCommit, _ := cmd.Flags().GetBool("no-commit")
	authorFlag, _ := cmd.Flags().GetString("author")
	tags, _ := cmd.Flags().GetStringArray("tag")

	body, _, err := resolveBody(cmd)
	if err != nil {
		return err
	}

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return err
	}

	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())
	if err := svc.EnsureDirectories(ctx); err != nil {
		return camperrors.Wrap(err, "ensuring intent directories")
	}

	// Fast path: note text provided as an argument
	if len(args) > 0 {
		author := "agent"
		if cmd.Flags().Changed("author") {
			author = authorFlag
		}
		opts := intent.CreateOptions{Title: args[0], Author: author, Body: body, Tags: tags}
		return runNoteCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts)
	}

	// No argument: non-TTY requires note text (can't launch TUI)
	if !navtui.IsTerminal() {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "note text required in non-interactive mode\n       Usage: camp intent note <text> [flags]")
	}

	author := git.GetUserName(ctx)
	if cmd.Flags().Changed("author") {
		author = authorFlag
	}

	conceptSvc := concept.NewService(campaignRoot, cfg.Concepts())
	model, err := runIntentNoteTUI(ctx, conceptSvc, tui.AddOptions{
		NoteMode:      true,
		Author:        author,
		CampaignRoot:  campaignRoot,
		Shortcuts:     navigationShortcuts(cfg),
		AvailableTags: cfg.IntentTags(),
	})
	if err != nil {
		return err
	}

	for _, saved := range model.SavedResults() {
		if err := runNoteCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, intent.CreateOptions{
			Title:  saved.Title,
			Author: saved.Author,
			Body:   saved.Body,
			Tags:   mergeTags(tags, saved.Tags),
		}); err != nil {
			return err
		}
	}

	result := model.Result()
	if result == nil {
		if len(model.SavedResults()) > 0 {
			return nil
		}
		return intent.ErrCancelled
	}

	return runNoteCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, intent.CreateOptions{
		Title:  result.Title,
		Author: result.Author,
		Body:   result.Body,
		Tags:   mergeTags(tags, result.Tags),
	})
}

func runIntentNoteTUI(ctx context.Context, conceptSvc concept.Service, opts tui.AddOptions) (*tui.IntentAddModel, error) {
	model := tui.NewIntentAddModel(ctx, conceptSvc, opts)

	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return nil, camperrors.Wrap(err, "TUI error")
	}

	m, ok := finalModel.(tui.IntentAddModel)
	if !ok {
		return nil, camperrors.Wrapf(camperrors.ErrInvalidInput, "unexpected model type: %T", finalModel)
	}

	return &m, nil
}

// mergeTags unions flag tags and overlay tags, preserving order and dropping
// duplicates and empties.
func mergeTags(a, b []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, list := range [][]string{a, b} {
		for _, t := range list {
			if t == "" || seen[t] {
				continue
			}
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func runNoteCapture(ctx context.Context, svc *intent.IntentService, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, noCommit bool, opts intent.CreateOptions) error {
	result, err := svc.CreateNote(ctx, opts)
	if err != nil {
		return camperrors.Wrap(err, "failed to create note")
	}

	return finalizeCreatedNote(ctx, result, intentsDir, cfg, campaignRoot, noCommit)
}

func finalizeCreatedNote(ctx context.Context, result *intent.Intent, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, noCommit bool) error {
	if err := appendIntentAuditEvent(ctx, intentsDir, audit.Event{
		Type:  audit.EventCreate,
		ID:    result.ID,
		Title: result.Title,
		To:    string(result.Status),
	}); err != nil {
		return err
	}

	fmt.Printf("✓ Note created: %s\n", result.Path)

	if !noCommit {
		opts := wkcmd.AmbientCommitOptions(ctx, campaignRoot, cfg.ID, os.Stderr)
		opts.NoteRef = noteRef(result.ID)
		opts.Files = commit.NormalizeFiles(campaignRoot, result.Path, audit.FilePath(intentsDir))
		opts.SelectiveOnly = true
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options:     opts,
			Action:      commit.IntentCreate,
			IntentTitle: result.Title,
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
		commit.WarnIfSkipped(os.Stderr, commitResult)
	}

	return nil
}
