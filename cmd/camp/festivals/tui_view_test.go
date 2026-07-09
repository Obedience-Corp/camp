package festivals

import (
	"context"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/ui"
)

func TestView_TwoLevelHeaders(t *testing.T) {
	ui.SetNoColor(true)
	m := newFestivalsTUIModel(context.Background(), "obc", []festivalItem{
		{Org: "obc", Campaign: "obey-campaign", Festival: "fa-1", Status: "active", Progress: progress{Completed: 1, Total: 1}},
		{Org: "obc", Campaign: "other", Festival: "fb-1", Status: "ready", Progress: progress{Completed: 0, Total: 3}},
	})
	m.width, m.height = 80, 20
	out := m.View()
	if !strings.Contains(out, "obc") || !strings.Contains(out, "obey-campaign") || !strings.Contains(out, "other") {
		t.Errorf("expected org + both campaign headers:\n%s", out)
	}
}
