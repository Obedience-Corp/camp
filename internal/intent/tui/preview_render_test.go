package tui

import "testing"

func TestRenderPreviewContentEmpty(t *testing.T) {
	if got := RenderPreviewContent("", 40); got != "No content" {
		t.Fatalf("empty raw = %q, want No content", got)
	}
	if got := RenderPreviewContent("---\nid: x\n---\n", 40); got != "No content" {
		t.Fatalf("frontmatter-only = %q, want No content", got)
	}
}

func TestApplyRenderedSetsTitleWithoutGlamour(t *testing.T) {
	p := NewPreviewPane(40, 12)
	p.ApplyRendered("T", "raw", "already-rendered")
	if p.Title() != "T" {
		t.Fatalf("title = %q, want T", p.Title())
	}
	if !p.HasContent() {
		t.Fatal("expected content after ApplyRendered")
	}
}
