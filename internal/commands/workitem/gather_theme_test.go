package workitem

import (
	"context"
	"errors"
	"testing"

	"github.com/charmbracelet/huh"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func TestPromptGatherSelectionUsesSharedThemeRunner(t *testing.T) {
	wantErr := errors.New("stop before interaction")
	called := false
	original := gatherFormRunner
	gatherFormRunner = func(_ context.Context, form *huh.Form) error {
		called = true
		if form == nil {
			t.Fatal("shared theme runner received nil form")
		}
		return wantErr
	}
	t.Cleanup(func() { gatherFormRunner = original })

	items := []wkitem.WorkItem{
		{Title: "One", RelativePath: "workflow/design/one"},
		{Title: "Two", RelativePath: "workflow/design/two"},
	}
	_, _, err := promptGatherSelection(context.Background(), items, "design", "Combined")
	if !called {
		t.Fatal("prompt did not use the shared theme runner")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("prompt error = %v, want wrapped runner error", err)
	}
}
