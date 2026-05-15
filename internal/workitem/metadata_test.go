package workitem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func mapFSWith(body string) fstest.MapFS {
	return fstest.MapFS{
		"workflow/feature/foo/.workitem": {Data: []byte(body)},
	}
}

const fixturePath = "workflow/feature/foo/.workitem"

func TestLoadMetadata_MissingFile(t *testing.T) {
	md, err := LoadMetadataFS(context.Background(), fstest.MapFS{}, fixturePath)
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if md != nil {
		t.Fatalf("missing file should return nil metadata, got %+v", md)
	}
}

func TestLoadMetadata_FullFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "workitem_full.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	md, err := LoadMetadataFS(context.Background(), mapFSWith(string(raw)), fixturePath)
	if err != nil {
		t.Fatalf("LoadMetadataFS: %v", err)
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
	body := `version: v1alpha4
kind: workitem
id: minimal-001
type: design
title: Minimal
`
	md, err := LoadMetadataFS(context.Background(), mapFSWith(body), fixturePath)
	if err != nil {
		t.Fatalf("LoadMetadataFS: %v", err)
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
			wantSubstr: "update .workitem `version:` to " + WorkitemSchemaVersion,
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
			md, err := LoadMetadataFS(context.Background(), mapFSWith(tc.body), fixturePath)
			if err == nil {
				t.Fatalf("expected error, got md=%+v", md)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error %q missing %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

func TestLoadMetadata_V1Alpha4BackwardCompat(t *testing.T) {
	body := `version: v1alpha4
kind: workitem
id: x
type: design
title: T
`
	md, err := LoadMetadataFS(context.Background(), mapFSWith(body), fixturePath)
	if err != nil {
		t.Fatalf("v1alpha4 should still parse: %v", err)
	}
	if md == nil || md.Version != "v1alpha4" {
		t.Errorf("expected v1alpha4 metadata, got %+v", md)
	}
}

func TestLoadMetadata_ContextCancelled(t *testing.T) {
	body := `version: v1alpha4
kind: workitem
id: x
type: design
title: T
`
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := LoadMetadataFS(ctx, mapFSWith(body), fixturePath)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v should wrap context.Canceled", err)
	}
}
