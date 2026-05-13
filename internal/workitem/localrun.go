package workitem

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// LocalRunProgress is camp's read-only view of a fest .workflow/ runtime.
// Camp and fest are separate Go modules; this is a thin duplicate of the
// fields camp needs from fest's localstore. Fest is the source of truth.
type LocalRunProgress struct {
	WorkflowID     string
	ActiveRunID    string
	RunStatus      string
	CurrentStep    int
	TotalSteps     int
	CompletedSteps int
	Blocked        bool
	DocHashChanged bool
}

type localWorkflowManifest struct {
	WorkflowID  string `yaml:"workflow_id"`
	ActiveRunID string `yaml:"active_run_id"`
	DocHash     string `yaml:"doc_hash"`
	DocPath     string `yaml:"doc_path"`
}

type localRunManifest struct {
	Status  string `yaml:"status"`
	Summary struct {
		CurrentStep    int  `yaml:"current_step"`
		TotalSteps     int  `yaml:"total_steps"`
		CompletedSteps int  `yaml:"completed_steps"`
		Blocked        bool `yaml:"blocked"`
	} `yaml:"summary"`
}

// LoadLocalRun reads .workflow/ progress for the workitem directory at dir.
// Returns (nil, nil) when no .workflow/workflow.yaml exists.
// Returns a wrapped error for parse failures; the discovery caller should
// log and continue rather than crashing the scan.
func LoadLocalRun(ctx context.Context, dir string) (*LocalRunProgress, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	manifestPath := filepath.Join(dir, ".workflow", "workflow.yaml")
	raw, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "reading %s", manifestPath)
	}

	var mf localWorkflowManifest
	if err := yaml.Unmarshal(raw, &mf); err != nil {
		return nil, camperrors.Wrapf(err, "parsing %s", manifestPath)
	}

	out := &LocalRunProgress{
		WorkflowID:  mf.WorkflowID,
		ActiveRunID: mf.ActiveRunID,
	}

	if mf.ActiveRunID != "" {
		runDir := filepath.Join(dir, ".workflow", "runs", mf.ActiveRunID)
		runYAML, runErr := os.ReadFile(filepath.Join(runDir, "run.yaml"))
		if runErr == nil {
			var rm localRunManifest
			if uErr := yaml.Unmarshal(runYAML, &rm); uErr != nil {
				return nil, camperrors.Wrapf(uErr, "parsing %s/run.yaml", runDir)
			}
			replayed := replayLocalRun(ctx, filepath.Join(runDir, "progress_events.jsonl"), rm)
			out.RunStatus = replayed.Status
			out.CurrentStep = replayed.Summary.CurrentStep
			out.CompletedSteps = replayed.Summary.CompletedSteps
			out.Blocked = replayed.Summary.Blocked
			out.TotalSteps = rm.Summary.TotalSteps
		} else if !errors.Is(runErr, os.ErrNotExist) {
			return nil, camperrors.Wrapf(runErr, "reading run manifest")
		}
	}

	// Detect doc-hash drift.
	if mf.DocHash != "" && mf.DocPath != "" {
		docFull := filepath.Join(dir, mf.DocPath)
		if cur, hErr := hashFile(docFull); hErr == nil && cur != mf.DocHash {
			out.DocHashChanged = true
		}
	}

	return out, nil
}

func replayLocalRun(ctx context.Context, eventsPath string, cache localRunManifest) localRunManifest {
	f, err := os.Open(eventsPath)
	if err != nil {
		return cache
	}
	defer f.Close()

	state := cache
	state.Summary.CurrentStep = 0
	state.Summary.CompletedSteps = 0
	state.Summary.Blocked = false
	if state.Status == "" {
		state.Status = "active"
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return state
		}
		var evt struct {
			EventType string `json:"event_type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			continue
		}
		switch evt.EventType {
		case "wf_step_start":
			state.Summary.CurrentStep++
		case "wf_step_done":
			state.Summary.CompletedSteps++
			state.Summary.Blocked = false
		case "wf_step_block", "wf_checkpoint_rejected":
			state.Summary.Blocked = true
		case "wf_checkpoint_approved":
			state.Summary.Blocked = false
		case "workflow_run_completed":
			state.Status = "completed"
		case "workflow_run_abandoned":
			state.Status = "abandoned"
		}
	}
	if state.Summary.Blocked && (state.Status == "" || state.Status == "active") {
		state.Status = "blocked"
	}
	return state
}

func hashFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
