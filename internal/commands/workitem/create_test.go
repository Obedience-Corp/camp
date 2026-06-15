package workitem

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/version"
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

func TestQuestFlagHelpTextMatchesProfile(t *testing.T) {
	for _, cmd := range []*cobra.Command{newCreateCommand(), newAdoptCommand()} {
		flag := cmd.Flags().Lookup("quest")
		if flag == nil {
			t.Fatalf("%s command missing --quest flag", cmd.Name())
		}
		switch version.Profile {
		case "dev":
			if !strings.Contains(flag.Usage, "defaults to CAMP_QUEST") {
				t.Fatalf("%s --quest usage = %q, want dev help", cmd.Name(), flag.Usage)
			}
		case "stable":
			if !strings.Contains(flag.Usage, "requires dev-profile camp") {
				t.Fatalf("%s --quest usage = %q, want stable forward-compatible help", cmd.Name(), flag.Usage)
			}
		default:
			t.Fatalf("unexpected version.Profile %q", version.Profile)
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

func TestRunCreate_DeriveFailureLeavesNoTargetAndRetrySucceeds(t *testing.T) {
	root := refQuestTestCampaign(t)
	restore := chdir(t, root)
	defer restore()
	t.Setenv("CAMP_QUEST", "")

	target := filepath.Join(root, "workflow", "design", "retryable")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := runCreate(ctx, cmd, "retryable", "design", "Retryable", "design-retryable-id", "", "", false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runCreate canceled error = %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("target dir after failed create stat err = %v, want not exist", statErr)
	}

	cmd = &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := runCreate(context.Background(), cmd, "retryable", "design", "Retryable", "design-retryable-id", "", "", false); err != nil {
		t.Fatalf("immediate retry runCreate() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".workitem")); err != nil {
		t.Fatalf("retry did not create marker: %v", err)
	}

	err = runCreate(context.Background(), cmd, "retryable", "design", "Retryable", "", "", "", false)
	if err == nil || !strings.Contains(err.Error(), "use `camp workitem adopt`") {
		t.Fatalf("second create error = %v, want adopt guidance", err)
	}
}

func TestRunCreate_PreExistingNonEmptyDirRequiresAdopt(t *testing.T) {
	root := refQuestTestCampaign(t)
	restore := chdir(t, root)
	defer restore()
	t.Setenv("CAMP_QUEST", "")

	target := filepath.Join(root, "workflow", "design", "legacy")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	contentPath := filepath.Join(target, "README.md")
	if err := os.WriteFile(contentPath, []byte("existing work\n"), 0o644); err != nil {
		t.Fatalf("write existing content: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := runCreate(context.Background(), cmd, "legacy", "design", "Legacy", "", "", "", false)
	if err == nil || !strings.Contains(err.Error(), "use `camp workitem adopt`") {
		t.Fatalf("runCreate existing dir error = %v, want adopt guidance", err)
	}
	if _, err := os.Stat(contentPath); err != nil {
		t.Fatalf("existing content was modified or removed: %v", err)
	}
}
