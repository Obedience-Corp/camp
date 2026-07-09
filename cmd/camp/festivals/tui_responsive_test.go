package festivals

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/ui"
)

func makeBrowserWithNFestivals(n int) festivalsTUIModel {
	orgs := []string{"obc", "devtools", "examples"}
	items := make([]festivalItem, 0, n)
	for i := 0; i < n; i++ {
		org := orgs[i%len(orgs)]
		campaign := fmt.Sprintf("campaign-%d", i%4)
		items = append(items, festivalItem{
			Org:      org,
			Campaign: campaign,
			Festival: fmt.Sprintf("festival-%03d", i),
			Status:   "active",
			Progress: progress{Completed: i, Total: n},
		})
	}
	return newFestivalsTUIModel(context.Background(), "obc", items)
}

func TestBrowser_NoLineExceedsWidth(t *testing.T) {
	ui.SetNoColor(true)
	m := makeBrowserWithNFestivals(50)
	for _, w := range []int{20, 30, 40, 60, 80, 120} {
		m.width, m.height = w, 24
		for _, line := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(line) > w {
				t.Errorf("width %d: line exceeds cw: %q (%d)", w, line, lipgloss.Width(line))
			}
		}
	}
}

func TestBrowser_ScrollKeepsCursorVisible(t *testing.T) {
	ui.SetNoColor(true)
	m := makeBrowserWithNFestivals(50)
	m.width, m.height = 80, 12 // small height forces scrolling
	for _, cursor := range []int{0, 10, 25, 49} {
		m.cursor = cursor
		out := m.View()
		if !strings.Contains(out, m.visible[cursor].Festival) {
			t.Errorf("cursor %d not visible in window:\n%s", cursor, out)
		}
	}
}
