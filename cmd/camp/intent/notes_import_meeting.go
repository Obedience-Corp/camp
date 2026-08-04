package intent

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	wkcmd "github.com/Obedience-Corp/camp/internal/commands/workitem"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	intentcore "github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// MeetingImportJSONVersion is the schema_version for import-meeting --json.
const MeetingImportJSONVersion = "intent-meeting-import/v1alpha1"

func init() {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "import-meeting <bundle-path>",
		Short: "Import a meeting bundle into notes/meetings/",
		Long: `Import a festival-voice (or compatible) meeting bundle as a note under
notes/meetings/ with a transcript sidecar in .transcripts/.

Re-importing the same bundle updates the existing note in place.

Examples:
  camp idea notes import-meeting ~/.obey/agents/voice/.../foo.meeting
  camp idea notes import-meeting ./bundle --summary-file summary.md --json
  camp idea notes import-meeting ./bundle --adopt-intent misfiled-id`,
	}
	jsonRequested := func() bool { return intentJSONRequested(cmd, &jsonOut) }
	cmd.Args = jsoncontract.Args(MeetingImportJSONVersion, jsonRequested, cobra.ExactArgs(1))
	cmd.RunE = jsoncontract.RunE(MeetingImportJSONVersion, jsonRequested, runImportMeeting)
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(MeetingImportJSONVersion, jsonRequested))
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	cmd.Flags().String("summary-file", "", "Path to summary markdown (overrides bundle summary.md)")
	cmd.Flags().String("summary", "", "Literal summary body")
	cmd.Flags().String("title", "", "Override note title")
	cmd.Flags().String("transcript-file", "", "Path to transcript file")
	cmd.Flags().String("adopt-intent", "", "Delete this lifecycle intent after successful import")
	cmd.Flags().String("author", "", "Author attribution")
	intentNotesCmd.AddCommand(cmd)
}

func runImportMeeting(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return err
	}
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intentcore.NewIntentService(campaignRoot, resolver.Intents())
	if err := svc.EnsureDirectories(ctx); err != nil {
		return camperrors.Wrap(err, "ensuring directories")
	}

	summaryFile, _ := cmd.Flags().GetString("summary-file")
	summary, _ := cmd.Flags().GetString("summary")
	title, _ := cmd.Flags().GetString("title")
	transcriptFile, _ := cmd.Flags().GetString("transcript-file")
	adopt, _ := cmd.Flags().GetString("adopt-intent")
	author, _ := cmd.Flags().GetString("author")
	jsonOut, _ := cmd.Flags().GetBool("json")
	adoptedPath := ""
	if adopt != "" {
		adopted, getErr := svc.Get(ctx, adopt)
		if getErr != nil {
			return camperrors.Wrap(getErr, "resolving intent to adopt")
		}
		adoptedPath = adopted.Path
	}

	result, err := svc.ImportMeeting(ctx, intentcore.ImportMeetingOptions{
		BundlePath:     args[0],
		Summary:        summary,
		SummaryFile:    summaryFile,
		Title:          title,
		TranscriptFile: transcriptFile,
		AdoptIntentID:  adopt,
		Author:         author,
	})
	if err != nil {
		return err
	}

	eventType := audit.EventCreate
	commitAction := commit.IntentCreate
	if result.UpdatedExisting {
		eventType = audit.EventEdit
		commitAction = commit.IntentEdit
	}
	if err := appendIntentAuditEvent(ctx, resolver.Intents(), audit.Event{
		Type:   eventType,
		ID:     result.Note.ID,
		Title:  result.Note.Title,
		To:     string(result.Note.Status),
		Reason: "imported meeting bundle",
	}); err != nil {
		return err
	}

	commitOpts := wkcmd.AmbientCommitOptions(ctx, campaignRoot, cfg.ID, os.Stderr)
	commitOpts.NoteRef = noteRef(result.Note.ID)
	commitOpts.Files = commit.NormalizeFiles(campaignRoot,
		result.Note.Path, result.TranscriptPath, adoptedPath, audit.FilePath(resolver.Intents()))
	commitOpts.SelectiveOnly = true
	commitResult := commit.Intent(ctx, commit.IntentOptions{
		Options:     commitOpts,
		Action:      commitAction,
		IntentTitle: result.Note.Title,
		Description: "Imported meeting bundle into notes/meetings",
	})
	commit.WarnIfSkipped(os.Stderr, commitResult)

	if intentJSONRequested(cmd, &jsonOut) {
		relNote, _ := pathutil.RelativeToRoot(campaignRoot, result.Note.Path)
		relTx, _ := pathutil.RelativeToRoot(campaignRoot, result.TranscriptPath)
		notePayload := map[string]any{
			"id":               result.Note.ID,
			"path":             relNote,
			"transcript":       relTx,
			"updated_existing": result.UpdatedExisting,
		}
		if result.Note.Meeting != nil {
			notePayload["duration_seconds"] = result.Note.Meeting.DurationSeconds
			notePayload["utterances"] = result.Note.Meeting.Utterances
			notePayload["speakers"] = result.Note.Meeting.Speakers
		}
		payload := map[string]any{
			"schema_version": MeetingImportJSONVersion,
			"generated_at":   time.Now().UTC(),
			"campaign_root":  campaignRoot,
			"note":           notePayload,
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	action := "imported"
	if result.UpdatedExisting {
		action = "updated"
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "✓ Meeting note %s: %s\n", action, result.Note.Path); err != nil {
		return camperrors.Wrap(err, "writing meeting import result")
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "  transcript: %s\n", result.TranscriptPath)
	return camperrors.Wrap(err, "writing meeting import result")
}
