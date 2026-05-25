package workitem

import (
	"regexp"
	"strings"
	"testing"
)

var refPattern = regexp.MustCompile(`^WI-[0-9a-f]{6}$`)

func TestDerive_Deterministic(t *testing.T) {
	id := "design-foo-2026-05-24"
	first := Derive(id)
	second := Derive(id)
	if first != second {
		t.Fatalf("Derive(%q) is non-deterministic: %q vs %q", id, first, second)
	}
	if !refPattern.MatchString(first) {
		t.Fatalf("Derive(%q) = %q does not match %s", id, first, refPattern)
	}
	if !strings.HasPrefix(first, RefPrefix) {
		t.Fatalf("Derive(%q) = %q does not have %q prefix", id, first, RefPrefix)
	}
}

func TestDerive_DifferentIDsDifferentRefs(t *testing.T) {
	a := Derive("design-foo-2026-05-24")
	b := Derive("design-bar-2026-05-24")
	if a == b {
		t.Fatalf("Derive collided across different inputs: %q == %q", a, b)
	}
}

func TestDeriveUnique_NoCollisionReturnsBase(t *testing.T) {
	id := "design-foo-2026-05-24"
	base := Derive(id)
	got := DeriveUnique(id, map[string]bool{})
	if got != base {
		t.Fatalf("DeriveUnique with empty existing should equal Derive: got %q, base %q", got, base)
	}
}

func TestDeriveUnique_AvoidsCollision(t *testing.T) {
	id := "design-foo-2026-05-24"
	base := Derive(id)
	existing := map[string]bool{base: true}
	got := DeriveUnique(id, existing)
	if got == base {
		t.Fatalf("DeriveUnique returned the colliding base ref: %q", got)
	}
	if existing[got] {
		t.Fatalf("DeriveUnique returned a ref still in the existing set: %q", got)
	}
	if !refPattern.MatchString(got) {
		t.Fatalf("DeriveUnique result %q does not match %s", got, refPattern)
	}
}

func TestDeriveUnique_HandlesMultipleCollisions(t *testing.T) {
	id := "design-foo-2026-05-24"
	existing := map[string]bool{
		Derive(id):        true,
		Derive(id + "#1"): true,
		Derive(id + "#2"): true,
	}
	got := DeriveUnique(id, existing)
	if existing[got] {
		t.Fatalf("DeriveUnique returned a colliding ref: %q", got)
	}
}

func TestRefsFromWorkitems(t *testing.T) {
	items := []WorkItem{
		{SourceMetadata: map[string]any{"ref": "WI-aaaaaa"}},
		{SourceMetadata: map[string]any{"ref": "WI-bbbbbb"}},
		{SourceMetadata: map[string]any{}},          // no ref
		{SourceMetadata: nil},                       // nil metadata
		{SourceMetadata: map[string]any{"ref": ""}}, // empty ref
	}
	set := RefsFromWorkitems(items)
	if len(set) != 2 {
		t.Fatalf("expected 2 refs, got %d (%v)", len(set), set)
	}
	if !set["WI-aaaaaa"] || !set["WI-bbbbbb"] {
		t.Fatalf("missing expected refs: %v", set)
	}
}
