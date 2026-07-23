package workitem

import (
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

func TestRenameBasename(t *testing.T) {
	cases := []struct {
		name    string
		newName string
		oldRel  string
		isFile  bool
		want    string
		wantErr bool
	}{
		{"file wrong extension rejected", "foo.txt", "workflow/explore/n.md", true, "", true},
		{"directory verbatim", "release-timeline", "workflow/design/timeline", false, "release-timeline", false},
		{"directory keeps dotted name", "v1.2", "workflow/design/timeline", false, "v1.2", false},
		{"file appends original extension", "weekly", "workflow/explore/note.md", true, "weekly.md", false},
		{"file keeps matching extension", "weekly.md", "workflow/explore/note.md", true, "weekly.md", false},
		{"file without original extension is verbatim", "readme2", "workflow/explore/README", true, "readme2", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := renameBasename(c.newName, c.oldRel, c.isFile)
			if c.wantErr {
				if err == nil {
					t.Fatalf("renameBasename(%q,%q,%v) = %q, want error", c.newName, c.oldRel, c.isFile, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("renameBasename(%q,%q,%v) unexpected error: %v", c.newName, c.oldRel, c.isFile, err)
			}
			if got != c.want {
				t.Fatalf("renameBasename(%q,%q,%v) = %q, want %q", c.newName, c.oldRel, c.isFile, got, c.want)
			}
		})
	}
}

func TestRenameKey(t *testing.T) {
	cases := []struct {
		name    string
		oldKey  string
		oldRel  string
		newRel  string
		want    string
		wantErr bool
	}{
		{"key must end with path", "design:workflow/design/other", "workflow/design/timeline", "workflow/design/release", "", true},
		{"design key rederived", "design:workflow/design/timeline", "workflow/design/timeline", "workflow/design/release", "design:workflow/design/release", false},
		{"file key rederived", "file:workflow/explore/note.md", "workflow/explore/note.md", "workflow/explore/weekly.md", "file:workflow/explore/weekly.md", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := renameKey(c.oldKey, c.oldRel, c.newRel)
			if c.wantErr {
				if err == nil {
					t.Fatalf("renameKey(%q,%q,%q) = %q, want error", c.oldKey, c.oldRel, c.newRel, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("renameKey unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("renameKey = %q, want %q", got, c.want)
			}
		})
	}
}

func TestEnsureRenamable(t *testing.T) {
	cases := []struct {
		name    string
		wfType  wkitem.WorkflowType
		wantErr bool
	}{
		{"festival rejected", wkitem.WorkflowTypeFestival, true},
		{"intent rejected", wkitem.WorkflowTypeIntent, true},
		{"design allowed", wkitem.WorkflowTypeDesign, false},
		{"explore allowed", wkitem.WorkflowTypeExplore, false},
		{"custom type allowed", wkitem.WorkflowType("feature"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ensureRenamable(&wkitem.WorkItem{WorkflowType: c.wfType})
			if c.wantErr != (err != nil) {
				t.Fatalf("ensureRenamable(%q) err=%v, wantErr=%v", c.wfType, err, c.wantErr)
			}
		})
	}
}

func TestRewriteScopePath(t *testing.T) {
	const oldRel, newRel = "workflow/design/alpha", "workflow/design/beta"
	cases := []struct {
		name   string
		path   string
		want   string
		wantOK bool
	}{
		{"unrelated path untouched", "projects/camp", "projects/camp", false},
		{"prefix-but-not-segment untouched", "workflow/design/alpha-notes", "workflow/design/alpha-notes", false},
		{"exact directory match", "workflow/design/alpha", "workflow/design/beta", true},
		{"subtree match", "workflow/design/alpha/sub/x", "workflow/design/beta/sub/x", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := rewriteScopePath(c.path, oldRel, newRel)
			if ok != c.wantOK || got != c.want {
				t.Fatalf("rewriteScopePath(%q) = (%q,%v), want (%q,%v)", c.path, got, ok, c.want, c.wantOK)
			}
		})
	}
}

func TestRekeyRenamePriorities(t *testing.T) {
	const oldKey, newKey = "design:workflow/design/alpha", "design:workflow/design/beta"

	t.Run("same key no change", func(t *testing.T) {
		store := priority.NewStore()
		priority.Set(store, oldKey, priority.High)
		if rekeyRenamePriorities(store, oldKey, oldKey) {
			t.Fatal("expected no change when keys are identical")
		}
	})

	t.Run("no entry no change", func(t *testing.T) {
		store := priority.NewStore()
		if rekeyRenamePriorities(store, oldKey, newKey) {
			t.Fatal("expected no change for absent key")
		}
	})

	t.Run("manual priority moves", func(t *testing.T) {
		store := priority.NewStore()
		priority.Set(store, oldKey, priority.High)
		if !rekeyRenamePriorities(store, oldKey, newKey) {
			t.Fatal("expected change")
		}
		if _, ok := store.ManualPriorities[oldKey]; ok {
			t.Fatal("old key should be cleared")
		}
		if store.ManualPriorities[newKey].Priority != priority.High {
			t.Fatalf("new key priority = %q, want high", store.ManualPriorities[newKey].Priority)
		}
	})

	t.Run("attention stage and group carry", func(t *testing.T) {
		store := priority.NewStore()
		priority.SetAttentionStage(store, oldKey, priority.AttentionCurrent)
		priority.SetGroup(store, oldKey, "auth")
		if !rekeyRenamePriorities(store, oldKey, newKey) {
			t.Fatal("expected change")
		}
		if _, ok := store.Attention[oldKey]; ok {
			t.Fatal("old key attention should be cleared")
		}
		entry := store.Attention[newKey]
		if entry.Stage != priority.AttentionCurrent || entry.Group != "auth" {
			t.Fatalf("new key attention = %+v, want stage=current group=auth", entry)
		}
	})
}

func TestRehomeRenameLinks(t *testing.T) {
	const (
		oldKey = "design:workflow/design/alpha"
		newKey = "design:workflow/design/beta"
		oldRel = "workflow/design/alpha"
		newRel = "workflow/design/beta"
	)

	t.Run("no matching references returns false", func(t *testing.T) {
		reg := &links.Links{Links: []links.Link{{
			ID: "l1", WorkitemID: "id", WorkitemKey: "design:workflow/design/other",
			Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/camp"},
		}}}
		if rehomeRenameLinks(reg, oldKey, newKey, oldRel, newRel) {
			t.Fatal("expected no change")
		}
	})

	t.Run("subject key migrates, stable id preserved", func(t *testing.T) {
		reg := &links.Links{Links: []links.Link{{
			ID: "l1", WorkitemID: "design-alpha-fixed", WorkitemKey: oldKey,
			Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/camp"},
		}}}
		if !rehomeRenameLinks(reg, oldKey, newKey, oldRel, newRel) {
			t.Fatal("expected change")
		}
		if reg.Links[0].WorkitemKey != newKey {
			t.Fatalf("workitem_key = %q, want %q", reg.Links[0].WorkitemKey, newKey)
		}
		if reg.Links[0].WorkitemID != "design-alpha-fixed" {
			t.Fatal("stable id must not change on rename")
		}
	})

	t.Run("scope path under renamed dir migrates", func(t *testing.T) {
		reg := &links.Links{Links: []links.Link{{
			ID: "l1", WorkitemID: "design-alpha-fixed", WorkitemKey: "design:workflow/design/other",
			Scope: links.LinkScope{Kind: links.ScopeCampaignPath, Path: "workflow/design/alpha/sub"},
		}}}
		if !rehomeRenameLinks(reg, oldKey, newKey, oldRel, newRel) {
			t.Fatal("expected change")
		}
		if reg.Links[0].Scope.Path != "workflow/design/beta/sub" {
			t.Fatalf("scope path = %q, want workflow/design/beta/sub", reg.Links[0].Scope.Path)
		}
	})
}
