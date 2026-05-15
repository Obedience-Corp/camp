package workitem

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Wire-schema event-type constants for .workflow/runs/<id>/progress_events.jsonl.
// Cross-repo contract with fest's localstore: changing a value here without
// matching change in fest will silently break replay. See D030 #9 / EVENT_SCHEMA.
// Future improvement: extract to projects/obey-shared/workflow/events.
const (
	wfEventStepStart          = "wf_step_start"
	wfEventStepDone           = "wf_step_done"
	wfEventStepSkip           = "wf_step_skip"
	wfEventStepBlock          = "wf_step_block"
	wfEventCheckpointApproved = "wf_checkpoint_approved"
	wfEventCheckpointRejected = "wf_checkpoint_rejected"
	wfEventRunCreated         = "workflow_run_created"
	wfEventRunStarted         = "workflow_run_started"
	wfEventRunCompleted       = "workflow_run_completed"
	wfEventRunAbandoned       = "workflow_run_abandoned"
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
func LoadLocalRun(ctx context.Context, dir string) (*LocalRunProgress, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolving %s", dir)
	}
	return LoadLocalRunFS(ctx, os.DirFS("/"), strings.TrimPrefix(abs, "/"))
}

// LoadLocalRunFS reads .workflow/ progress from fsys rooted at base.
// Used by tests so parsing/replay is exercised via fstest.MapFS without
// touching the host filesystem (D029).
func LoadLocalRunFS(ctx context.Context, fsys fs.FS, base string) (*LocalRunProgress, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	manifestPath := filepath.Join(base, ".workflow", "workflow.yaml")
	raw, err := fs.ReadFile(fsys, manifestPath)
	if errors.Is(err, fs.ErrNotExist) {
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
		runDir := filepath.Join(base, ".workflow", "runs", mf.ActiveRunID)
		runYAML, runErr := fs.ReadFile(fsys, filepath.Join(runDir, "run.yaml"))
		if runErr == nil {
			var rm localRunManifest
			if uErr := yaml.Unmarshal(runYAML, &rm); uErr != nil {
				return nil, camperrors.Wrapf(uErr, "parsing %s/run.yaml", runDir)
			}
			replayed, repErr := replayLocalRunFSE(ctx, fsys, filepath.Join(runDir, "progress_events.jsonl"), rm)
			if repErr != nil {
				return nil, repErr
			}
			out.RunStatus = replayed.Status
			out.CurrentStep = replayed.Summary.CurrentStep
			out.CompletedSteps = replayed.Summary.CompletedSteps
			out.Blocked = replayed.Summary.Blocked
			out.TotalSteps = rm.Summary.TotalSteps
		} else if !errors.Is(runErr, fs.ErrNotExist) {
			return nil, camperrors.Wrapf(runErr, "reading run manifest")
		}
	}

	if mf.DocHash != "" && mf.DocPath != "" {
		docFull := filepath.Join(base, mf.DocPath)
		if cur, hErr := hashFileFS(fsys, docFull); hErr == nil && cur != mf.DocHash {
			out.DocHashChanged = true
		}
	}

	return out, nil
}

func replayLocalRunFSE(ctx context.Context, fsys fs.FS, eventsPath string, cache localRunManifest) (localRunManifest, error) {
	f, err := fsys.Open(eventsPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cache, nil
		}
		return cache, camperrors.Wrapf(err, "open %s", eventsPath)
	}
	defer func() { _ = f.Close() }()

	// Events are authoritative. Discard the cached status and replay from
	// scratch so a stale "completed" or "abandoned" in run.yaml does not
	// override the actual current state.
	state := cache
	state.Summary.CurrentStep = 0
	state.Summary.CompletedSteps = 0
	state.Summary.Blocked = false
	state.Status = "active"

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return state, err
		}
		var evt struct {
			EventType string `json:"event_type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			continue
		}
		switch evt.EventType {
		case wfEventRunCreated, wfEventRunStarted:
		case wfEventStepStart:
			state.Summary.CurrentStep++
		case wfEventStepDone:
			state.Summary.CompletedSteps++
			state.Summary.Blocked = false
		case wfEventStepSkip:
			state.Summary.CompletedSteps++
			state.Summary.Blocked = false
		case wfEventStepBlock, wfEventCheckpointRejected:
			state.Summary.Blocked = true
		case wfEventCheckpointApproved:
			state.Summary.Blocked = false
		case wfEventRunCompleted:
			state.Status = "completed"
		case wfEventRunAbandoned:
			state.Status = "abandoned"
		}
	}
	if err := scanner.Err(); err != nil {
		return state, camperrors.Wrapf(err, "scanning %s", eventsPath)
	}
	if state.Summary.Blocked && state.Status == "active" {
		state.Status = "blocked"
	}
	return state, nil
}

func hashFileFS(fsys fs.FS, path string) (string, error) {
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
