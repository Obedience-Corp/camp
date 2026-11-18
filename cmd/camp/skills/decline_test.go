package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
	"github.com/spf13/cobra"
)

func TestDeclineFailure_KeepsTheRestoreCommand(t *testing.T) {
	restore := "mkdir -p .campaign/skills/festival-app && cp -R .campaign/skills/.backup/festival-app-x/. .campaign/skills/festival-app/"
	res := &intskills.DeclineResult{Restore: &intskills.Undo{Command: restore}}
	err := declineFailure("festival-app", errors.New("write manifest: no space left on device"), res)
	want := "restore the removed files: " + restore
	if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "no space left on device") {
		t.Fatalf("human error = %q, want the cause and %q", err, want)
	}

	var stderr bytes.Buffer
	cmd := &cobra.Command{Use: "decline"}
	cmd.SetErr(&stderr)
	_ = jsoncontract.RenderError(cmd, SkillsDeclineJSONVersion, err)
	var envelope jsoncontract.ErrorEnvelope
	if jsonErr := json.Unmarshal(stderr.Bytes(), &envelope); jsonErr != nil {
		t.Fatalf("envelope: %v\n%s", jsonErr, stderr.String())
	}
	if !strings.Contains(envelope.Error.Message, want) || envelope.Error.Hint != want {
		t.Fatalf("envelope = %+v, want the restore command in message and hint", envelope.Error)
	}

	plain := errors.New("boom")
	if got := declineFailure("festival-app", plain, &intskills.DeclineResult{}); got != plain {
		t.Fatalf("a failure with nothing to restore was changed: %v", got)
	}
}
