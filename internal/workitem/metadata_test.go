package workitem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"gopkg.in/yaml.v3"
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
			name: "wrong version lists current supported version",
			body: `version: v1alpha3
kind: workitem
id: x
type: design
title: T
`,
			wantSubstr: "v1alpha7",
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
		{
			name: "invalid ref shape",
			body: `version: v1alpha6
kind: workitem
id: x
type: design
title: T
ref: NOT-A-VALID-REF-12345
`,
			wantSubstr: "ref must match WI-<6 hex>",
		},
		{
			name: "ref with wrong length",
			body: `version: v1alpha6
kind: workitem
id: x
type: design
title: T
ref: WI-abc
`,
			wantSubstr: "ref must match WI-<6 hex>",
		},
		{
			name: "invalid quest_id shape",
			body: `version: v1alpha6
kind: workitem
id: x
type: design
title: T
quest_id: not_a_quest_id
`,
			wantSubstr: "quest_id must match qst_",
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

func TestValidateMetadata_RefShape(t *testing.T) {
	base := Metadata{Version: "v1alpha6", Kind: MetadataKind, ID: "x", Type: "design"}
	cases := []struct {
		name      string
		mutate    func(*Metadata)
		wantErr   bool
		wantField string
	}{
		{"empty ref ok", func(m *Metadata) {}, false, ""},
		{"valid ref", func(m *Metadata) { m.Ref = "WI-abcdef" }, false, ""},
		{"uppercase hex", func(m *Metadata) { m.Ref = "WI-ABCDEF" }, true, "ref"},
		{"wrong length", func(m *Metadata) { m.Ref = "WI-abc" }, true, "ref"},
		{"missing WI prefix", func(m *Metadata) { m.Ref = "abcdef" }, true, "ref"},
		{"embedded dash junk", func(m *Metadata) { m.Ref = "WI-abc-def" }, true, "ref"},
		{"review repro junk", func(m *Metadata) { m.Ref = "NOT-A-VALID-REF-12345" }, true, "ref"},
		{"valid quest_id", func(m *Metadata) { m.QuestID = "qst_20260525_abc" }, false, ""},
		{"empty quest_id ok", func(m *Metadata) {}, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			tc.mutate(&m)
			err := validateMetadata(&m)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %s, got nil", tc.name)
				}
				var verr *camperrors.ValidationError
				if !errors.As(err, &verr) {
					t.Fatalf("expected ValidationError, got %T: %v", err, err)
				}
				if verr.Field != tc.wantField {
					t.Errorf("Field = %q, want %q", verr.Field, tc.wantField)
				}
			} else if err != nil {
				t.Fatalf("expected no error for %s, got %v", tc.name, err)
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

func TestMetadata_TagsProjectsRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   Metadata
	}{
		{"nil tags and projects", Metadata{Version: WorkitemSchemaVersion, Kind: MetadataKind, ID: "x", Type: "design"}},
		{"populated tags and projects", Metadata{
			Version:  WorkitemSchemaVersion,
			Kind:     MetadataKind,
			ID:       "x",
			Type:     "design",
			Tags:     []string{"public-launch", "schema"},
			Projects: []string{"projects/camp", "projects/fest"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := yaml.Marshal(&tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var out Metadata
			if err := yaml.Unmarshal(raw, &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(out.Tags, tc.in.Tags) {
				t.Errorf("Tags = %#v, want %#v", out.Tags, tc.in.Tags)
			}
			if !reflect.DeepEqual(out.Projects, tc.in.Projects) {
				t.Errorf("Projects = %#v, want %#v", out.Projects, tc.in.Projects)
			}
		})
	}
}

func TestValidateMetadata_VersionAcceptance(t *testing.T) {
	cases := []struct {
		name              string
		version           string
		wantErr           bool
		wantForwardCompat bool
	}{
		{"unknown old version rejected", "v1alpha3", true, false},
		{"non-alpha series rejected", "v1beta1", true, false},
		{"malformed alpha suffix rejected", "v1alpha7x", true, false},
		{"v1alpha4 accepted", "v1alpha4", false, false},
		{"v1alpha5 accepted", "v1alpha5", false, false},
		{"v1alpha6 accepted", "v1alpha6", false, false},
		{"v1alpha7 accepted", "v1alpha7", false, false},
		{"v1alpha8 accepted", "v1alpha8", false, false},
		{"future version accepted via forward-compat", "v1alpha12", false, true},
		{"leading-zero future version accepted via forward-compat", "v1alpha09", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := "version: " + tc.version + "\nkind: workitem\nid: x\ntype: design\ntitle: T\n"
			md, err := LoadMetadataFS(context.Background(), mapFSWith(body), fixturePath)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %s, got md=%+v", tc.version, md)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected %s to load, got %v", tc.version, err)
			}
			if got := IsForwardCompatVersion(md.Version); got != tc.wantForwardCompat {
				t.Errorf("IsForwardCompatVersion(%q) = %v, want %v", md.Version, got, tc.wantForwardCompat)
			}
		})
	}
}

func TestValidateMetadata_TagsProjects(t *testing.T) {
	base := Metadata{Version: WorkitemSchemaVersion, Kind: MetadataKind, ID: "x", Type: "design"}
	cases := []struct {
		name      string
		mutate    func(*Metadata)
		wantErr   bool
		wantField string
	}{
		{"malformed tag", func(m *Metadata) { m.Tags = []string{"Public-Launch"} }, true, "tags"},
		{"duplicate tag", func(m *Metadata) { m.Tags = []string{"public-launch", "public-launch"} }, true, "tags"},
		{"empty project", func(m *Metadata) { m.Projects = []string{""} }, true, "projects"},
		{"absolute project", func(m *Metadata) { m.Projects = []string{"/etc/passwd"} }, true, "projects"},
		{"escaping project", func(m *Metadata) { m.Projects = []string{"../outside"} }, true, "projects"},
		{"duplicate project", func(m *Metadata) { m.Projects = []string{"projects/camp", "projects/camp"} }, true, "projects"},
		{"nonexistent project accepted", func(m *Metadata) { m.Projects = []string{"projects/does-not-exist"} }, false, ""},
		{"well-formed tags and projects", func(m *Metadata) {
			m.Tags = []string{"ux", "perf"}
			m.Projects = []string{"projects/camp", "projects/fest"}
		}, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			tc.mutate(&m)
			err := validateMetadata(&m)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("expected no error for %s, got %v", tc.name, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			var verr *camperrors.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
			if verr.Field != tc.wantField {
				t.Errorf("Field = %q, want %q", verr.Field, tc.wantField)
			}
		})
	}
}

func TestLoadMetadata_V1Alpha7MarkerUnaffectedByReaderChanges(t *testing.T) {
	body := `version: v1alpha7
kind: workitem
id: design-thing-2026-07-18
type: design
title: A thing
ref: WI-abcdef
quest_id: qst_20260718_abc123
`
	md, err := LoadMetadataFS(context.Background(), mapFSWith(body), fixturePath)
	if err != nil {
		t.Fatalf("v1alpha7 marker should load: %v", err)
	}
	if md == nil {
		t.Fatal("expected metadata")
	}
	if md.Version != "v1alpha7" {
		t.Errorf("Version = %q, want v1alpha7", md.Version)
	}
	if md.ID != "design-thing-2026-07-18" {
		t.Errorf("ID = %q", md.ID)
	}
	if md.Type != "design" {
		t.Errorf("Type = %q", md.Type)
	}
	if md.Title != "A thing" {
		t.Errorf("Title = %q", md.Title)
	}
	if md.Ref != "WI-abcdef" {
		t.Errorf("Ref = %q", md.Ref)
	}
	if md.QuestID != "qst_20260718_abc123" {
		t.Errorf("QuestID = %q", md.QuestID)
	}
	if md.Tags != nil {
		t.Errorf("Tags = %#v, want nil for a marker with no tags", md.Tags)
	}
	if md.Projects != nil {
		t.Errorf("Projects = %#v, want nil for a marker with no projects", md.Projects)
	}
}
