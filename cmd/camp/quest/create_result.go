package quest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/quest"
)

// Creation results preserve identity even when binding or committing fails after
// the quest was written. Consumers must never repeat creation to retry a reveal.
type questCreateResult struct {
	SchemaVersion string       `json:"schema_version"`
	CampaignRoot  string       `json:"campaign_root"`
	Quest         *quest.Quest `json:"quest"`
	Created       bool         `json:"created"`
	Binding       string       `json:"binding"`
	Commit        string       `json:"commit"`
	Error         *string      `json:"error"`
}

func finishQuestCreation(cmd *cobra.Command, qctx *questCommandContext, created *quest.MutationResult, boundPath string, noCommit bool) error {
	result := questCreateResult{
		SchemaVersion: QuestCreateJSONVersion, CampaignRoot: qctx.campaignRoot,
		Created: true, Binding: "notRequested", Commit: "notRun",
	}
	item, err := questForJSON(qctx, created.Quest)
	if err != nil {
		return err
	}
	result.Quest = item
	linked, err := bindWorkitem(cmd.Context(), qctx, created, boundPath)
	if err != nil {
		result.Binding = "failed"
		return writeQuestCreateResult(cmd, result, err)
	}
	if boundPath != "" {
		result.Binding = "completed"
	}
	result.Quest, err = questForJSON(qctx, linked.Quest)
	if err != nil {
		return err
	}
	emitQuestCreated(cmd, qctx, linked)
	if !questCreateJSON {
		printQuestCreated(qctx, linked, boundPath)
	}
	result.Commit = "skipped"
	if !noCommit {
		err = autoCommitQuestQuiet(cmd.Context(), qctx, commit.QuestCreate, linked, "Created quest", questCreateJSON)
		if err != nil {
			result.Commit = "failed"
			return writeQuestCreateResult(cmd, result, camperrors.Wrap(err, "quest created, but auto-commit failed"))
		}
		// The existing commit policy determines whether a git commit was needed.
		result.Commit = "completed"
	}
	if err := writeQuestCreateResult(cmd, result, nil); err != nil {
		return err
	}
	open, _ := cmd.Flags().GetBool("open")
	if open {
		revealCreatedQuest(cmd, qctx.campaignRoot, linked.Quest.ID)
	}
	return nil
}

func writeQuestCreateResult(cmd *cobra.Command, result questCreateResult, cause error) error {
	if questCreateJSON {
		if cause != nil {
			message := cause.Error()
			result.Error = &message
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return camperrors.Wrap(err, "write quest creation result")
		}
	}
	return cause
}

func revealCreatedQuest(cmd *cobra.Command, root, id string) {
	ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Second)
	defer cancel()
	requestID, err := uuid.NewV7()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: quest %s was created, but app reveal could not be prepared; run camp app quest open %s --camp %q\n", id, id, root)
		return
	}
	recovery := fmt.Sprintf("camp app quest open %s --camp %q", id, root)
	// No lookup or app probing occurs unless the caller explicitly requested --open.
	child := exec.CommandContext(ctx, "camp-app", "quest", "open", id,
		"--camp", root, "--focus", "target", "--timeout", "2s", "--json", "--request-id", requestID.String())
	child.Dir = root
	child.WaitDelay = 100 * time.Millisecond
	// Do not forward a plugin envelope into the core command's stdout, or trust
	// arbitrary child diagnostics. Local creation remains successful on failure.
	if err := child.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || isExecNotFound(err) {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: quest %s was created, but camp-app was not available for reveal; run %s\n", id, recovery)
			return
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: quest %s was created, but app reveal was not confirmed (request %s); inspect camp app operation show %s, then run %s if needed\n", id, requestID, requestID, recovery)
	}
}

func isExecNotFound(err error) bool {
	var pathErr *exec.Error
	return errors.As(err, &pathErr) && pathErr.Err == exec.ErrNotFound
}
