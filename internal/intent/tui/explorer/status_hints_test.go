package explorer

import (
	"strings"
	"testing"
)

func TestRenderStatusBarHints_GroupHeaderShowsExpand(t *testing.T) {
	m := makeTestModel(2, 1)
	m.layoutMode = layoutNormal
	m.cursorItem = -1
	hints := m.renderStatusBarHints()
	if !strings.Contains(hints, "enter/l expand") {
		t.Fatalf("group header hints missing expand: %q", hints)
	}
	if strings.Contains(hints, "space gather") {
		t.Fatalf("group header should not emphasize space gather: %q", hints)
	}
}

func TestRenderStatusBarHints_ItemShowsGather(t *testing.T) {
	m := makeTestModel(2, 1)
	m.layoutMode = layoutNormal
	m.cursorItem = 0
	hints := m.renderStatusBarHints()
	if !strings.Contains(hints, "space gather") {
		t.Fatalf("item hints missing space gather: %q", hints)
	}
	if strings.Contains(hints, "enter/l expand") {
		t.Fatalf("item hints should not show group expand: %q", hints)
	}
}
