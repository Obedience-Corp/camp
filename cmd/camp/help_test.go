package main

import (
	"strings"
	"testing"
)

func TestCleanFlagUsagesStripsInternalNoOptSentinel(t *testing.T) {
	input := "  -c, --campaign string[=\"          pick\"]\x00Target campaign by name or ID"

	got := cleanFlagUsages(input)

	if strings.ContainsRune(got, '\x00') {
		t.Fatalf("cleanFlagUsages left NUL sentinel in output: %q", got)
	}
	if strings.Contains(got, "string[=") {
		t.Fatalf("cleanFlagUsages left optional default in output: %q", got)
	}
	if !strings.Contains(got, "--campaign string   Target campaign by name or ID") {
		t.Fatalf("cleanFlagUsages output = %q", got)
	}
}

func TestCleanFlagUsagesLeavesNormalUsageUnchanged(t *testing.T) {
	input := "      --verbose   enable verbose output"
	if got := cleanFlagUsages(input); got != input {
		t.Fatalf("cleanFlagUsages(%q) = %q", input, got)
	}
}
