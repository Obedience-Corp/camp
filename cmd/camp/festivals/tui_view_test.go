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

func TestView_TopBarCountsVisibleRows(t *testing.T) {
	ui.SetNoColor(true)
	m := newFestivalsTUIModel(context.Background(), "obc", []festivalItem{
		{Org: "obc", Campaign: "alpha", Festival: "active-1", Status: "active"},
		{Org: "obc", Campaign: "beta", Festival: "planning-1", Status: "planning"},
		{Org: "obc", Campaign: "beta", Festival: "planning-2", Status: "planning"},
	})
	m.activeOnly = true
	m.rebuildVisible()

	got := m.topBar()
	if !strings.Contains(got, "1 festival across 1 campaign") {
		t.Fatalf("top bar should count visible rows and campaigns, got %q", got)
	}
}

func TestView_ScrollModeFlattensHeaders(t *testing.T) {
	ui.SetNoColor(true)
	m := newFestivalsTUIModel(context.Background(), "obc", []festivalItem{
		{Org: "obc", Campaign: "alpha", Festival: "alpha-1", Status: "active"},
		{Org: "obc", Campaign: "alpha", Festival: "alpha-2", Status: "active"},
		{Org: "obc", Campaign: "beta", Festival: "beta-1", Status: "active"},
		{Org: "obc", Campaign: "beta", Festival: "beta-2", Status: "active"},
		{Org: "obc", Campaign: "beta", Festival: "beta-3", Status: "active"},
	})
	m.cursor = 3
	m.width = 80
	m.height = 10

	lines := m.bodyLines(m.layout())
	joined := strings.Join(lines, "\n")
	if strings.Count(joined, "obc") != 1 || !strings.Contains(joined, "obc / beta") {
		t.Fatalf("scroll mode should show only the breadcrumb header, got:\n%s", joined)
	}
	if !strings.Contains(joined, "beta-2") {
		t.Fatalf("scroll mode should keep the selected row visible, got:\n%s", joined)
	}
}
