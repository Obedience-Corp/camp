package quest

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// ledgerEmitter is the quest service's view of the campaign ledger, satisfied by
// *ledger.Emitter. A nil emitter disables emission.
type ledgerEmitter interface {
	Emit(ctx context.Context, kind ledgerkit.Kind, scope ledgerkit.Scope, opts ...ledger.Option)
}

// SetLedger wires the campaign-ledger emitter (dev-gated command layer only).
func (s *Service) SetLedger(e ledgerEmitter) { s.emitter = e }

// emitTransition records a quest status change; a move to completed is a
// completed event, everything else a transitioned event.
func (s *Service) emitTransition(ctx context.Context, q *Quest, from, to Status) {
	if s.emitter == nil || q == nil {
		return
	}
	kind := ledgerkit.KindTransitioned
	if to == StatusCompleted {
		kind = ledgerkit.KindCompleted
	}
	s.emitter.Emit(ctx, kind, ledgerkit.Scope{Quest: q.ID},
		ledger.WithPayload(map[string]any{"from": string(from), "to": string(to)}))
}
