package workitem

import (
	"context"
	"testing"
	"testing/fstest"
)

func testCtx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

func TestEmitCandidate_BuiltinAlwaysEmits(t *testing.T) {
	ok, reason := emitCandidateFS(fstest.MapFS{}, "design", "anywhere/x")
	if !ok || reason != "builtin" {
		t.Errorf("design built-in: ok=%v reason=%q", ok, reason)
	}
}

func TestEmitCandidate_BuiltinForAllFour(t *testing.T) {
	for _, wt := range []string{"intent", "design", "explore", "festival"} {
		t.Run(wt, func(t *testing.T) {
			ok, reason := emitCandidateFS(fstest.MapFS{}, wt, "nonexistent/path")
			if !ok || reason != "builtin" {
				t.Errorf("%s should be built-in, got ok=%v reason=%q", wt, ok, reason)
			}
		})
	}
}

func TestEmitCandidate_CustomTypeRequiresMarker(t *testing.T) {
	fsys := fstest.MapFS{
		"workflow/incident/y/.workitem": {Data: []byte("")},
	}
	ok, reason := emitCandidateFS(fsys, "incident", "workflow/incident/x")
	if ok || reason != "no-marker" {
		t.Errorf("custom type without marker: ok=%v reason=%q", ok, reason)
	}
	ok, reason = emitCandidateFS(fsys, "incident", "workflow/incident/y")
	if !ok || reason != "marker" {
		t.Errorf("custom type with marker: ok=%v reason=%q", ok, reason)
	}
}

const validV1Alpha4Metadata = `version: v1alpha4
kind: workitem
id: bar-001
type: incident
title: Bar
`

func TestDiscover_CustomTypeRequiresMarker(t *testing.T) {
	fsys := fstest.MapFS{
		"workflow/incident/has-marker/.workitem": {Data: []byte(validV1Alpha4Metadata)},
		"workflow/incident/no-marker/README.md":  {Data: []byte("hi")},
		"workflow/feature/builtin/README.md":     {Data: []byte("hi")},
	}

	cases := []struct {
		typeDir string
		dir     string
		want    bool
	}{
		{"incident", "workflow/incident/has-marker", true},
		{"incident", "workflow/incident/no-marker", false},
		{"feature", "workflow/feature/builtin", false},
	}
	for _, c := range cases {
		got, _ := emitCandidateFS(fsys, c.typeDir, c.dir)
		if got != c.want {
			t.Errorf("emitCandidateFS(%q, %q) = %v, want %v", c.typeDir, c.dir, got, c.want)
		}
	}
}

func TestDiscover_MalformedWorkitemSkipped(t *testing.T) {
	fsys := fstest.MapFS{
		"workflow/incident/bad/.workitem": {Data: []byte("::: not yaml")},
	}
	_, err := LoadMetadataFS(testCtx(t), fsys, "workflow/incident/bad/.workitem")
	if err == nil {
		t.Fatal("expected parse error for malformed yaml")
	}
}
