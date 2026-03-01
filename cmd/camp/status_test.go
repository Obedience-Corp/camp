package main

import (
	"testing"
)

func TestExtractShowRefs_NotPresent(t *testing.T) {
	args := []string{"--short", "-s"}
	filtered, showRefs := extractShowRefs(args)

	if showRefs {
		t.Error("showRefs should be false when --show-refs not present")
	}
	if len(filtered) != 2 {
		t.Errorf("filtered args length = %d, want 2", len(filtered))
	}
}

func TestExtractShowRefs_Present(t *testing.T) {
	args := []string{"--short", "--show-refs", "-s"}
	filtered, showRefs := extractShowRefs(args)

	if !showRefs {
		t.Error("showRefs should be true when --show-refs present")
	}
	if len(filtered) != 2 {
		t.Errorf("filtered args length = %d, want 2", len(filtered))
	}
	for _, arg := range filtered {
		if arg == "--show-refs" {
			t.Error("--show-refs should be removed from filtered args")
		}
	}
}

func TestExtractShowRefs_OnlyShowRefs(t *testing.T) {
	args := []string{"--show-refs"}
	filtered, showRefs := extractShowRefs(args)

	if !showRefs {
		t.Error("showRefs should be true")
	}
	if len(filtered) != 0 {
		t.Errorf("filtered args length = %d, want 0", len(filtered))
	}
}

func TestExtractShowRefs_Empty(t *testing.T) {
	args := []string{}
	filtered, showRefs := extractShowRefs(args)

	if showRefs {
		t.Error("showRefs should be false for empty args")
	}
	if len(filtered) != 0 {
		t.Errorf("filtered args length = %d, want 0", len(filtered))
	}
}
