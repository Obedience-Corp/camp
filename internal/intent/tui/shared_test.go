package tui

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestMoveStatusOptions_PipelineOrder(t *testing.T) {
	want := []intent.Status{
		intent.StatusInbox,
		intent.StatusReady,
		intent.StatusActive,
		intent.StatusDone,
		intent.StatusKilled,
		intent.StatusArchived,
		intent.StatusSomeday,
	}
	if len(moveStatusOptions) != len(want) {
		t.Fatalf("len(moveStatusOptions) = %d, want %d", len(moveStatusOptions), len(want))
	}
	for i, opt := range moveStatusOptions {
		if opt.status != want[i] {
			t.Errorf("moveStatusOptions[%d] = %s, want %s (pipeline is inbox → ready → active → done)", i, opt.status, want[i])
		}
	}
}
