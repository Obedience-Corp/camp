package intent

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intentcore "github.com/Obedience-Corp/camp/internal/intent"
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

	if intentJSONRequested(cmd, &jsonOut) {
		relNote, _ := pathutil.RelativeToRoot(campaignRoot, result.Note.Path)
		relTx, _ := pathutil.RelativeToRoot(campaignRoot, result.TranscriptPath)
		payload := map[string]any{
			"schema_version": MeetingImportJSONVersion,
			"generated_at":   time.Now().UTC(),
			"campaign_root":  campaignRoot,
			"note": map[string]any{
				"id":               result.Note.ID,
				"path":             relNote,
				"transcript":       relTx,
				"updated_existing": result.UpdatedExisting,
			},
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	action := "imported"
	if result.UpdatedExisting {
		action = "updated"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✓ Meeting note %s: %s\n", action, result.Note.Path)
	fmt.Fprintf(cmd.OutOrStdout(), "  transcript: %s\n", result.TranscriptPath)
	return nil
}
