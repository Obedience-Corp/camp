package crawl

import (
	"context"
	"errors"
	"testing"
)

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
