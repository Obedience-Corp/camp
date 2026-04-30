package crawl

import (
	"context"
	"fmt"
)

// FakePrompt is a deterministic Prompt implementation for tests.
// Each scripted response is consumed in order. If a script runs out
// before the test completes, the prompt returns an error so the
// failure is loud rather than silent.
//
// FakePrompt is exported so other packages (e.g., internal/dungeon
// and internal/intent/crawl) can drive the shared runner from
// their own tests without depending on a TTY.
type FakePrompt struct {
	// ActionScript is consumed by SelectAction in order. Each entry
	// becomes the returned (Action, error) pair.
	ActionScript []ActionResponse
	// DestinationScript is consumed by SelectDestination in order.
	DestinationScript []DestinationResponse
	// ReasonScript is consumed by Reason in order.
	ReasonScript []ReasonResponse

	actionIdx, destIdx, reasonIdx int
}

// ActionResponse is one scripted SelectAction return.
type ActionResponse struct {
	Action Action
	Err    error
}

// DestinationResponse is one scripted SelectDestination return.
//
// To simulate the "esc" gesture (back to first menu), set Option to
// the zero value and Err to nil.
type DestinationResponse struct {
	Option Option
	Err    error
}

// ReasonResponse is one scripted Reason return.
type ReasonResponse struct {
	Reason string
	Err    error
}

// SelectAction returns the next scripted ActionResponse.
func (p *FakePrompt) SelectAction(_ context.Context, _ Item, _ []Option) (Action, error) {
	if p.actionIdx >= len(p.ActionScript) {
		return "", fmt.Errorf("FakePrompt: SelectAction script exhausted (call %d)", p.actionIdx+1)
	}
	r := p.ActionScript[p.actionIdx]
	p.actionIdx++
	return r.Action, r.Err
}

// SelectDestination returns the next scripted DestinationResponse.
func (p *FakePrompt) SelectDestination(_ context.Context, _ Item, _ []Option) (Option, error) {
	if p.destIdx >= len(p.DestinationScript) {
		return Option{}, fmt.Errorf("FakePrompt: SelectDestination script exhausted (call %d)", p.destIdx+1)
	}
	r := p.DestinationScript[p.destIdx]
	p.destIdx++
	return r.Option, r.Err
}

// Reason returns the next scripted ReasonResponse.
func (p *FakePrompt) Reason(_ context.Context, _ Item, _ Option) (string, error) {
	if p.reasonIdx >= len(p.ReasonScript) {
		return "", fmt.Errorf("FakePrompt: Reason script exhausted (call %d)", p.reasonIdx+1)
	}
	r := p.ReasonScript[p.reasonIdx]
	p.reasonIdx++
	return r.Reason, r.Err
}

// ActionsConsumed reports how many SelectAction calls have been made.
func (p *FakePrompt) ActionsConsumed() int { return p.actionIdx }

// DestinationsConsumed reports how many SelectDestination calls have been made.
func (p *FakePrompt) DestinationsConsumed() int { return p.destIdx }

// ReasonsConsumed reports how many Reason calls have been made.
func (p *FakePrompt) ReasonsConsumed() int { return p.reasonIdx }
