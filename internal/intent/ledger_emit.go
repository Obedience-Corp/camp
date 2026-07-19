package intent

import (
	"context"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// ledgerEmitter is the intent service's view of the campaign ledger: it is
// satisfied by *ledger.Emitter and mocked in tests. A nil emitter disables
// emission, so service unit tests that never set one do not touch the ledger.
type ledgerEmitter interface {
	Emit(ctx context.Context, kind ledgerkit.Kind, scope ledgerkit.Scope, opts ...ledger.Option)
}

// SetLedger wires the campaign-ledger emitter. Commands set it after resolving
// the campaign (see the intent command layer); service tests leave it nil.
func (s *IntentService) SetLedger(e ledgerEmitter) { s.emitter = e }

// emitCreated records an intent creation (D003 boundary: after the file write).
func (s *IntentService) emitCreated(ctx context.Context, in *Intent) {
	if s.emitter == nil || in == nil {
		return
	}
	s.emitter.Emit(ctx, ledgerkit.KindCreated, ledgerkit.Scope{Intent: in.ID},
		ledger.WithWhy(in.Title),
		ledger.WithPayload(map[string]any{"status": string(in.Status), "type": string(in.Type)}))
}

// emitTransitioned records an intent status change with from/to in the payload,
// making a file move a first-class ledger event.
func (s *IntentService) emitTransitioned(ctx context.Context, in *Intent, from, to Status) {
	if s.emitter == nil || in == nil {
		return
	}
	s.emitter.Emit(ctx, ledgerkit.KindTransitioned, ledgerkit.Scope{Intent: in.ID},
		ledger.WithPayload(map[string]any{"from": string(from), "to": string(to)}))
}

// emitClaimed records an intent assignment: who claimed it and any work refs
// stamped alongside the claim (e.g. a PR URL recorded once one is opened).
func (s *IntentService) emitClaimed(ctx context.Context, in *Intent) {
	if s.emitter == nil || in == nil {
		return
	}
	s.emitter.Emit(ctx, ledgerkit.KindClaimed, ledgerkit.Scope{Intent: in.ID},
		ledger.WithWhy(fmt.Sprintf("claimed by %s", in.AssignedTo)),
		ledger.WithPayload(map[string]any{"assigned_to": in.AssignedTo, "work_ref": in.WorkRef}))
}

// emitReleased records an intent assignment being cleared, keeping the prior
// assignee in the payload so the ledger retains who last held the claim.
func (s *IntentService) emitReleased(ctx context.Context, in *Intent, previousAssignee string) {
	if s.emitter == nil || in == nil {
		return
	}
	s.emitter.Emit(ctx, ledgerkit.KindReleased, ledgerkit.Scope{Intent: in.ID},
		ledger.WithPayload(map[string]any{"previous_assignee": previousAssignee}))
}
