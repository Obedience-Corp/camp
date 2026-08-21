package quest

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRewriteCampaignRelPath(t *testing.T) {
	t.Parallel()

	moves := []relMove{
		{src: "workflow/design/auth-flow", dst: "workflow/design/unified-auth/auth-flow"},
		{src: "workflow/design/auth-tokens", dst: "workflow/design/unified-auth/auth-tokens"},
	}

	tests := []struct {
		name string
		path string
		want string
		ok   bool
	}{
		{
			name: "exact directory match",
			path: "workflow/design/auth-flow",
			want: "workflow/design/unified-auth/auth-flow",
			ok:   true,
		},
		{
			name: "nested file under moved directory",
			path: "workflow/design/auth-flow/README.md",
			want: "workflow/design/unified-auth/auth-flow/README.md",
			ok:   true,
		},
		{
			name: "sibling prefix is not a match",
			path: "workflow/design/auth-flow-extra",
			want: "workflow/design/auth-flow-extra",
			ok:   false,
		},
		{
			name: "unrelated path",
			path: "workflow/design/billing",
			want: "workflow/design/billing",
			ok:   false,
		},
		{
			name: "empty path",
			path: "",
			want: "",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := rewriteCampaignRelPath(tt.path, moves)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("rewriteCampaignRelPath(%q) = %q, %v; want %q, %v", tt.path, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestRewriteCampaignRelPath_ChainedMoves(t *testing.T) {
	t.Parallel()

	moves := []relMove{
		{src: "workflow/design/item", dst: "dungeon/item"},
		{src: "dungeon/item", dst: "dungeon/completed/item"},
	}
	got, ok := rewriteCampaignRelPath("workflow/design/item/README.md", moves)
	if !ok {
		t.Fatal("expected chained rewrite")
	}
	if got != "dungeon/completed/item/README.md" {
		t.Fatalf("got %q, want dungeon/completed/item/README.md", got)
	}
}

func TestRewriteQuestLinkPaths(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	q := &Quest{
		ID:        "qst_test",
		Name:      "test",
		Status:    StatusOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Links: []Link{
			{Path: "workflow/design/auth-flow", Type: "design", AddedAt: now},
			{Path: "docs/guide.md", Type: "document", AddedAt: now},
		},
	}
	moves := []relMove{
		{src: "workflow/design/auth-flow", dst: "festivals/ready/auth-flow"},
	}

	if !rewriteQuestLinkPaths(q, moves) {
		t.Fatal("expected quest links to change")
	}
	if q.Links[0].Path != "festivals/ready/auth-flow" {
		t.Fatalf("workitem link = %q", q.Links[0].Path)
	}
	if q.Links[0].Type != "design" {
		t.Fatalf("type should stay as stored, got %q", q.Links[0].Type)
	}
	if q.Links[1].Path != "docs/guide.md" {
		t.Fatalf("unrelated link mutated: %q", q.Links[1].Path)
	}
}

func TestRewriteQuestLinkPaths_NoMatch(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	q := &Quest{
		ID:        "qst_test",
		Name:      "test",
		Status:    StatusOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Links: []Link{
			{Path: "projects/camp", Type: "project", AddedAt: now},
		},
	}
	if rewriteQuestLinkPaths(q, []relMove{{src: "workflow/design/a", dst: "workflow/design/b"}}) {
		t.Fatal("unrelated links must not report a change")
	}
}

func TestRewriteLinksForMoves_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RewriteLinksForMoves(ctx, "/nonexistent", []PathMove{{Src: "a", Dst: "b"}})
	if err == nil {
		t.Fatal("expected cancelled context error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestToCampaignRel_RejectsEscape(t *testing.T) {
	t.Parallel()

	_, err := toCampaignRel("/campaign", "/outside")
	if err == nil {
		t.Fatal("expected error for path outside campaign root")
	}
}

func TestRewriteLinksForMoves_MissingQuestDir(t *testing.T) {
	t.Parallel()

	got, err := RewriteLinksForMoves(context.Background(), "/no-such-campaign-root", []PathMove{
		{Src: "workflow/design/a", Dst: "workflow/design/b"},
	})
	if err != nil {
		t.Fatalf("missing quest dir should be a no-op, got %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}
