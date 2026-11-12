// Package approvals is camp's side of the approvals inbox: how a pending
// request (pkg/approvalkit) is acted on.
//
// The ledger is untrusted input. It is a plain file any process in the camp can
// append to, so nothing here runs anything a request says to run. A request
// names a source kind; the command for that kind is fixed in this package, and
// the directory it runs in is checked to be inside the camp.
package approvals

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"

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

// statusRunner runs the source's status command in dir and returns its stdout.
// A variable so tests do not need fest installed.
var statusRunner = func(ctx context.Context, dir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, festBinary, "workflow", "status", "--json")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
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
	if status.Complete || status.CurrentStep == nil {
		return Readiness{Reason: "its source is no longer waiting"}
	}
	// A phase's workflow and its gate share a directory and both number their
	// steps from 1, so the step alone cannot tell them apart: a request for
	// workflow step 1 must not be actionable because the gate is at its step 1.
	if (p.SourceKind == approvalkit.SourcePhaseGate) != (status.Mode == statusModeGate) {
		return Readiness{Reason: "its source is no longer waiting"}
	}
	if *status.CurrentStep != p.Step {
		return Readiness{Reason: "its source is at step " + strconv.Itoa(*status.CurrentStep) + ", not step " + strconv.Itoa(p.Step)}
	}
	return Readiness{Actionable: true, Dir: dir}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
