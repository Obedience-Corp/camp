package crawl

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// FakePrompt is a deterministic Prompt implementation for tests.
// Each scripted response is consumed in order. If a script runs out
// before the test completes, the prompt returns an error so the
// failure is loud rather than silent.
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

func TestFakePrompt_SelectActionConsumesScript(t *testing.T) {
	p := &FakePrompt{
		ActionScript: []ActionResponse{
			{Action: ActionKeep},
			{Action: ActionMove},
			{Action: ActionQuit},
		},
	}
	for i, want := range []Action{ActionKeep, ActionMove, ActionQuit} {
		got, err := p.SelectAction(context.Background(), Item{}, nil)
		if err != nil {
			t.Fatalf("call %d: unexpected error %v", i, err)
		}
		if got != want {
			t.Fatalf("call %d: got %q, want %q", i, got, want)
		}
	}
	if p.ActionsConsumed() != 3 {
		t.Errorf("ActionsConsumed = %d, want 3", p.ActionsConsumed())
	}
}

func TestFakePrompt_SelectActionExhaustedFailsLoudly(t *testing.T) {
	p := &FakePrompt{}
	if _, err := p.SelectAction(context.Background(), Item{}, nil); err == nil {
		t.Fatal("expected error when script is empty")
	}
}

func TestFakePrompt_SelectDestinationReturnsBackOnZero(t *testing.T) {
	p := &FakePrompt{
		DestinationScript: []DestinationResponse{
			{Option: Option{}}, // back gesture
			{Option: Option{Action: ActionMove, Target: "ready"}},
		},
	}

	got, err := p.SelectDestination(context.Background(), Item{}, nil)
	if err != nil {
		t.Fatalf("call 1 error = %v", err)
	}
	if got.Target != "" {
		t.Errorf("call 1 target = %q, want empty (back gesture)", got.Target)
	}

	got, err = p.SelectDestination(context.Background(), Item{}, nil)
	if err != nil {
		t.Fatalf("call 2 error = %v", err)
	}
	if got.Target != "ready" {
		t.Errorf("call 2 target = %q, want %q", got.Target, "ready")
	}
}

func TestFakePrompt_AbortPropagates(t *testing.T) {
	p := &FakePrompt{
		ActionScript: []ActionResponse{{Err: ErrAborted}},
	}
	_, err := p.SelectAction(context.Background(), Item{}, nil)
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("expected ErrAborted, got %v", err)
	}
}

func TestFakePrompt_ReasonScript(t *testing.T) {
	p := &FakePrompt{
		ReasonScript: []ReasonResponse{
			{Reason: "superseded"},
			{Reason: ""}, // cancelled
		},
	}

	r, err := p.Reason(context.Background(), Item{}, Option{Target: "dungeon/archived"})
	if err != nil || r != "superseded" {
		t.Fatalf("first reason: got %q, %v", r, err)
	}

	r, err = p.Reason(context.Background(), Item{}, Option{Target: "dungeon/archived"})
	if err != nil || r != "" {
		t.Fatalf("second reason: got %q, %v (expected empty cancel)", r, err)
	}
}

func TestIsAborted(t *testing.T) {
	if !IsAborted(ErrAborted) {
		t.Fatal("IsAborted(ErrAborted) should be true")
	}
	if IsAborted(errors.New("other")) {
		t.Fatal("IsAborted(other) should be false")
	}
}
