package rename

import (
	"strings"
	"testing"
)

func TestNormalizeCurrent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		current string
		want    string
		wantRel string
		wantErr string
	}{
		{name: "bare", current: "old-api", want: "old-api", wantRel: "projects/old-api"},
		{name: "exact path", current: "projects/old-api", want: "old-api", wantRel: "projects/old-api"},
		{name: "nested rejected", current: "projects/team/old-api", wantErr: "top-level"},
		{name: "other root rejected", current: "vendor/old-api", wantErr: "top-level"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, gotRel, err := normalizeCurrent(tc.current, "projects")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("normalizeCurrent() error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want || gotRel != tc.wantRel {
				t.Fatalf("normalizeCurrent() = (%q, %q), want (%q, %q)", got, gotRel, tc.want, tc.wantRel)
			}
		})
	}
}

func TestRewriteWorkitemOnlyChangesTypedProjects(t *testing.T) {
	t.Parallel()
	raw := []byte(`version: v1alpha8
kind: workitem
title: projects/old should remain prose
projects:
  - projects/old
  - projects/old/subdir
  - projects/other
`)
	out, count, err := rewriteMetadata("workitem-yaml", raw, "old", "new", "projects/old", "projects/new", "")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	text := string(out)
	for _, want := range []string{"title: projects/old should remain prose", "- projects/new", "- projects/new/subdir", "- projects/other"} {
		if !strings.Contains(text, want) {
			t.Errorf("rewritten workitem missing %q:\n%s", want, text)
		}
	}
}

func TestRewriteFreshRejectsDestinationKeyCollision(t *testing.T) {
	t.Parallel()
	raw := []byte("projects:\n  old: {branch: one}\n  new: {branch: two}\n")
	_, _, err := rewriteMetadata("fresh-yaml", raw, "old", "new", "projects/old", "projects/new", "")
	if err == nil || !strings.Contains(err.Error(), `fresh project "new" already exists`) {
		t.Fatalf("rewriteMetadata() error = %v", err)
	}
}

func TestRewriteCampaignProjectUpdatesExplicitRemote(t *testing.T) {
	t.Parallel()
	raw := []byte("projects:\n  - name: old\n    path: projects/old\n    url: git@example.com:org/old.git\n")
	out, count, err := rewriteMetadata("campaign-yaml", raw, "old", "new", "projects/old", "projects/new", "git@example.com:org/new.git")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3; output=%s", count, out)
	}
	text := string(out)
	for _, want := range []string{"name: new", "path: projects/new", "url: git@example.com:org/new.git"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

func TestRewriteLeverageMigratesIdentityAndPaths(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"projects":{"old":{"path":"projects/old","include":true},"child":{"path":"projects/old/sub","monorepo_path":"projects/old"}}}`)
	out, count, err := rewriteMetadata("leverage-json", raw, "old", "new", "projects/old", "projects/new", "")
	if err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("count = %d, want 4; output=%s", count, out)
	}
	text := string(out)
	if strings.Contains(text, `"old"`) || !strings.Contains(text, `"new"`) || !strings.Contains(text, "projects/new/sub") {
		t.Fatalf("unexpected output: %s", text)
	}
}

func TestReplaceManagedPathMovesConventionalWorktreeOnly(t *testing.T) {
	t.Parallel()
	got, changed := replaceManagedPath("projects/worktrees/old/feature", "old", "new", "projects/old", "projects/new")
	if !changed || got != "projects/worktrees/new/feature" {
		t.Fatalf("replaceManagedPath() = (%q, %v)", got, changed)
	}
	got, changed = replaceManagedPath("/tmp/old/feature", "old", "new", "projects/old", "projects/new")
	if changed || got != "/tmp/old/feature" {
		t.Fatalf("external path changed: (%q, %v)", got, changed)
	}
}
