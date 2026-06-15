package workitem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func TestValidateSlug(t *testing.T) {
	cases := []struct {
		slug string
		ok   bool
	}{
		// path-safe, project doesn't enforce style
		{"foo", true},
		{"foo-bar", true},
		{"foo_bar", true},
		{"foo123", true},
		{"a", true},
		{"Foo", true},
		{"PascalCase", true},
		{"camelCase", true},
		{"v1.2", true},
		{"payment.processor.v2", true},
		{"2026-Q1-roadmap", true},
		{"foo!", true},
		{"foo@bar", true},
		// path-unsafe — rejected
		{"", false},
		{"foo bar", false},
		{"foo\tbar", false},
		{"foo/bar", false},
		{`foo\bar`, false},
		{`\backslash`, false},
		{".hidden", false},
		{".", false},
		{"..", false},
		{"-foo", false},
		{"foo\x00bar", false},
		{"foo\x1fbar", false},
		{strings.Repeat("a", 81), false},
	}
	for _, c := range cases {
		err := validateSlug(c.slug)
		if (err == nil) != c.ok {
			t.Errorf("validateSlug(%q) error=%v, want ok=%v", c.slug, err, c.ok)
		}
	}
}

func TestValidateParentPath(t *testing.T) {
	cases := []struct {
		path string
		ok   bool
	}{
		{"workflow/feature", true},
		{"workflow/incident", true},
		{"/abs/path", false},
		{"../escape", false},
	}
	for _, c := range cases {
		err := validateParentPath(c.path)
		if (err == nil) != c.ok {
			t.Errorf("validateParentPath(%q) error=%v, want ok=%v", c.path, err, c.ok)
		}
	}
}

func TestRunCreateWritesWorkitemMarker(t *testing.T) {
	root := refQuestTestCampaign(t)
	restore := chdir(t, root)
	defer restore()
	t.Setenv("CAMP_QUEST", "")

	cmd := &cobra.Command{}
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	if err := runCreate(context.Background(), cmd, "atomic-marker", "design", "Atomic Marker", "", "", "", false); err != nil {
		t.Fatalf("runCreate() error = %v", err)
	}

	meta := loadMarker(t, filepath.Join(root, "workflow", "design", "atomic-marker", ".workitem"))
	if meta.Version != wkitem.WorkitemSchemaVersion {
		t.Fatalf("version = %q, want %q", meta.Version, wkitem.WorkitemSchemaVersion)
	}
	if meta.Title != "Atomic Marker" {
		t.Fatalf("title = %q, want %q", meta.Title, "Atomic Marker")
	}
	if meta.Ref == "" {
		t.Fatal("expected ref to be written")
	}
}
