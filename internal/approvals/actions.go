// Package approvals is camp's side of the approvals inbox: how a pending
// request (pkg/approvalkit) is acted on.
//
// The ledger is untrusted input. It is a plain file any process in the camp can
// append to, and it arrives from other machines by git pull, so nothing here
// runs anything a request says to run. A request names a source kind; the
// command for that kind is fixed in this package, and the directory it runs in
// is checked to be inside the camp.
//
// What a forged request can still do: choose which directory inside the camp
// fest is started in. A camp holds cloned repositories, so that directory can
// be one whose contents the user did not write, and fest will read its workflow
// files and configuration there. Check runs 'fest workflow status' in it, and a
// decision runs 'fest workflow approve' or 'reject' in it only after Check has
// confirmed that fest itself reports a workflow there waiting at the recorded
// step. Neither command runs hooks or a judge: those need --auto, which is
// never passed.
package approvals

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/pkg/approvalkit"
)

// Decision is what a human can do with a pending request.
type Decision string

const (
	Approve Decision = "approve"
	Reject  Decision = "reject"
)

// festBinary is the command that owns fest checkpoints. A variable so tests
// can point it elsewhere.
var festBinary = "fest"

// festSourceKinds are the request kinds whose source is a fest workflow
// checkpoint, decided by 'fest workflow approve' and 'fest workflow reject'
// run from the source directory.
var festSourceKinds = map[string]bool{
	approvalkit.SourceFestivalCheckpoint:   true,
	approvalkit.SourceStandaloneCheckpoint: true,
	approvalkit.SourcePhaseGate:            true,
}

// Supported reports whether camp knows how to act on a request of this kind.
// An unknown kind is still listed; it just cannot be approved or rejected from
// the inbox.
func Supported(sourceKind string) bool { return festSourceKinds[sourceKind] }

// Command returns the fixed command that records decision at p's source.
//
// The command is chosen from p.SourceKind alone and is pinned to p.Step with
// --expect-step, so the source refuses, changing nothing, if it is no longer
// at the checkpoint the user was shown. reason is required for a rejection and
// ignored for an approval. Nothing from the request other than its step number
// reaches the command line.
func Command(p approvalkit.Pending, decision Decision, reason string) ([]string, error) {
	if !Supported(p.SourceKind) {
		return nil, camperrors.New("camp does not know how to act on a " + quoteKind(p.SourceKind) + " request")
	}
	if p.Step < 1 {
		return nil, camperrors.New("request has no step to pin the decision to")
	}
	step := strconv.Itoa(p.Step)
	switch decision {
	case Approve:
		return []string{festBinary, "workflow", "approve", "--expect-step", step}, nil
	case Reject:
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return nil, camperrors.New("a rejection needs a reason")
		}
		return []string{festBinary, "workflow", "reject", "--expect-step", step, "--reason", reason}, nil
	default:
		return nil, camperrors.New("unknown decision: " + string(decision))
	}
}

func quoteKind(kind string) string {
	if strings.TrimSpace(kind) == "" {
		return "source-less"
	}
	return strconv.Quote(kind)
}

// Readiness says whether a request can be acted on from this machine, and why
// not when it cannot.
type Readiness struct {
	Actionable bool
	// Dir is the absolute directory the source's command runs in. Set only
	// when Actionable.
	Dir string
	// Reason explains a request that is not actionable, in words fit to show
	// beside it.
	Reason string
	// Over is true only when the source, read on this machine, shows the
	// request can never be decided: the source has finished, or it is past the
	// recorded step. It is what a bulk cleanup may act on.
	//
	// A source that is behind the recorded step is not over. Source state can
	// lag the ledger after a pull, so "behind" usually means this checkout has
	// not caught up, and the request is live on the machine that made it. A
	// withdrawal is written to the shared ledger and would clear it there too.
	Over bool
}

// sourceStatus is the part of 'fest workflow status --json' camp reads.
type sourceStatus struct {
	// Mode is "gate" when the phase's GATES.md is what is current, which
	// happens once its WORKFLOW.md is complete.
	Mode        string `json:"mode"`
	CurrentStep *int   `json:"current_step"`
	Complete    bool   `json:"complete"`
}

// statusModeGate is the status mode fest reports for a phase gate.
const statusModeGate = "gate"

// statusTimeout bounds one source status call. Listing asks every pending
// request's source, and requests whose source is gone stay listed until they
// are withdrawn, so one source that hangs must not hold up the whole list.
const statusTimeout = 5 * time.Second

// statusRunner runs the source's status command in dir and returns its stdout.
// A variable so tests do not need fest installed.
var statusRunner = func(ctx context.Context, dir string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, festBinary, "workflow", "status", "--json")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, camperrors.New("it did not answer within " + statusTimeout.String())
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, camperrors.New(msg)
	}
	return out, nil
}

// Check reports whether p can be acted on from this machine.
//
// A request travels between machines in the committed ledger, but its source
// may not: the directory can be missing here (a worktree that lives on another
// machine), or the source's own state can lag the ledger after a pull. Either
// way the request is still worth listing, and must not be acted on. Check also
// catches a request whose directory is not inside the camp at all, which a
// genuine source never produces.
func Check(ctx context.Context, campaignRoot string, p approvalkit.Pending) Readiness {
	if !Supported(p.SourceKind) {
		return Readiness{Reason: "camp cannot act on a " + quoteKind(p.SourceKind) + " request"}
	}
	dir, err := approvalkit.ResolveCwd(campaignRoot, p.Cwd)
	if err != nil {
		if !approvalkit.IsCampRelative(p.Cwd) {
			return Readiness{Reason: "its directory is outside this camp"}
		}
		return Readiness{Reason: "its directory is not on this machine"}
	}
	out, err := statusRunner(ctx, dir)
	if err != nil {
		return Readiness{Reason: "its source could not be read: " + firstLine(err.Error())}
	}
	var status sourceStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return Readiness{Reason: "its source status could not be parsed"}
	}
	if status.Complete {
		return Readiness{Reason: "its source has finished", Over: true}
	}
	if status.CurrentStep == nil {
		return Readiness{Reason: "its source is not at any step"}
	}
	// A phase's workflow and its gate share a directory and both number their
	// steps from 1, so the step alone cannot tell them apart: a request for
	// workflow step 1 must not be actionable because the gate is at its step 1.
	gateCurrent := status.Mode == statusModeGate
	if p.SourceKind == approvalkit.SourcePhaseGate && !gateCurrent {
		// The gate only becomes current once the workflow is complete, so
		// here the workflow is still running: behind, not over.
		return Readiness{Reason: "its phase has not reached the gate on this machine"}
	}
	if p.SourceKind != approvalkit.SourcePhaseGate && gateCurrent {
		// The gate being current means the workflow this request belongs to
		// is complete.
		return Readiness{Reason: "its workflow has finished; the phase gate is current", Over: true}
	}
	if *status.CurrentStep != p.Step {
		return Readiness{
			Reason: "its source is at step " + strconv.Itoa(*status.CurrentStep) + ", not step " + strconv.Itoa(p.Step),
			// Past the step: over. Before it: this checkout is behind.
			Over: *status.CurrentStep > p.Step,
		}
	}
	return Readiness{Actionable: true, Dir: dir}
}

// firstLine returns the first line of a source's error output with control
// characters removed: it is shown beside the request, and it comes from a
// program run in a directory the ledger chose.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return approvalkit.CleanLine(s)
}

// CheckAll runs Check for every request, a few at a time, and returns the
// results in the same order. Each check starts a source process, so running
// them one after another makes a long list slow to open.
func CheckAll(ctx context.Context, campaignRoot string, pending []approvalkit.Pending) []Readiness {
	const workers = 4
	out := make([]Readiness, len(pending))
	sem := make(chan struct{}, workers)
	done := make(chan struct{})
	for i := range pending {
		go func(i int) {
			sem <- struct{}{}
			out[i] = Check(ctx, campaignRoot, pending[i])
			<-sem
			done <- struct{}{}
		}(i)
	}
	for range pending {
		<-done
	}
	return out
}
