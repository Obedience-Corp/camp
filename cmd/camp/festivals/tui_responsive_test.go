package festivals

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/uitest"
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
		uitest.AssertBounded(t, m.View(), w, 24)
	}
}

func TestBrowser_HasNoPhantomTrailingRow(t *testing.T) {
	m := makeBrowserWithNFestivals(50)
	m.width, m.height = 80, 24
	out := m.View()
	if strings.HasSuffix(out, "\n") {
		t.Fatalf("full-screen festivals view ends with a phantom row: %q", out)
	}
	if got := len(strings.Split(out, "\n")); got > m.height {
		t.Fatalf("rendered %d rows for terminal height %d", got, m.height)
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
