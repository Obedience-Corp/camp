package quest

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateSlug(t *testing.T) {
	got := GenerateSlug("Research: Best practices for API design")
	want := "research-best-practices-for-api"
	if got != want {
		t.Fatalf("GenerateSlug() = %q, want %q", got, want)
	}
}

func TestGenerateDirectorySlug(t *testing.T) {
	ts := time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC)
	got := GenerateDirectorySlug("Add OAuth2 support for Google", ts)
	want := "20260119-add-oauth2-support-for-google"
	if got != want {
		t.Fatalf("GenerateDirectorySlug() = %q, want %q", got, want)
	}
}

func TestGenerateIDFormat(t *testing.T) {
	ts := time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC)
	got, err := GenerateID(ts)
	if err != nil {
		t.Fatalf("GenerateID() error = %v", err)
	}
	if !strings.HasPrefix(got, "qst_20260119_") || len(got) != len("qst_20260119_abcdef") {
		t.Fatalf("GenerateID() = %q, want qst_20260119_<6 chars>", got)
	}
}

