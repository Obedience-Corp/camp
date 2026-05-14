package workitem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeWorkitem(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, MetadataFilename), []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func TestLoadMetadata_MissingFile(t *testing.T) {
	dir := t.TempDir()
	md, err := LoadMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if md != nil {
		t.Fatalf("missing file should return nil metadata, got %+v", md)
	}
}

func TestLoadMetadata_FullFixture(t *testing.T) {
	dir := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("testdata", "workitem_full.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	writeWorkitem(t, dir, string(raw))

	md, err := LoadMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}
	if md == nil {
		t.Fatal("expected non-nil metadata")
	}

	if md.Version != WorkitemSchemaVersion {
		t.Errorf("Version = %q, want %q", md.Version, WorkitemSchemaVersion)
	}
	if md.Kind != "workitem" {
		t.Errorf("Kind = %q, want %q", md.Kind, "workitem")
	}
	if md.ID != "design-thin-start-workflow-ladder-2026-04-25" {
		t.Errorf("ID = %q", md.ID)
	}
	if md.Type != "design" {
		t.Errorf("Type = %q", md.Type)
	}
	if md.Title != "Thin-start workflow ladder" {
		t.Errorf("Title = %q", md.Title)
	}
}

func TestLoadMetadata_MinimalRequiredFields(t *testing.T) {
	dir := t.TempDir()
	writeWorkitem(t, dir, `version: v1alpha4
kind: workitem
id: minimal-001
type: design
title: Minimal
`)
	md, err := LoadMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}
	if md == nil {
		t.Fatal("expected metadata")
	}
}

func TestLoadMetadata_Errors(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantSubstr string
	}{
		{
			name:       "invalid yaml",
			body:       "not: valid: yaml: ::\n",
			wantSubstr: "parsing",
		},
		{
			name: "wrong version",
			body: `version: v1alpha3
kind: workitem
id: x
type: design
title: T
`,
			wantSubstr: "schema version",
		},
		{
			name: "v1alpha3 rejected with upgrade hint",
			body: `version: v1alpha3
kind: workitem
id: x
type: design
title: T
`,
			wantSubstr: "update .workitem `version:` to v1alpha4",
		},
		{
			name: "wrong kind",
			body: `version: v1alpha4
kind: festival
id: x
type: design
title: T
`,
			wantSubstr: "kind",
		},
		{
			name: "missing id",
			body: `version: v1alpha4
kind: workitem
type: design
title: T
`,
			wantSubstr: "id is empty",
		},
		{
			name: "missing type",
			body: `version: v1alpha4
kind: workitem
id: x
title: T
`,
			wantSubstr: "type is empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeWorkitem(t, dir, tc.body)
			md, err := LoadMetadata(context.Background(), dir)
			if err == nil {
				t.Fatalf("expected error, got md=%+v", md)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error %q missing %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

func TestLoadMetadata_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	writeWorkitem(t, dir, `version: v1alpha4
kind: workitem
id: x
type: design
title: T
`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := LoadMetadata(ctx, dir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
