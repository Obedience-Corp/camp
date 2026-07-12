package intent

import (
	"context"

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
