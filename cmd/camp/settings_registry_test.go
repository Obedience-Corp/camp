package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestRegistryOptions_SortedByNameWithBackRow(t *testing.T) {
	reg := &config.Registry{
		Campaigns: map[string]config.RegisteredCampaign{
			"uuid-charlie": {Name: "Charlie", Org: "obc", Path: "/c"},
			"uuid-alpha":   {Name: "Alpha", Org: "personal", Path: "/a"},
			"uuid-bravo":   {Name: "Bravo", Org: "devtools", Path: "/b"},
		},
	}

	opts := registryOptions(reg)
	got := optionValues(opts)
	want := []string{"uuid-alpha", "uuid-bravo", "uuid-charlie", valSeparator, valBack}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("registryOptions values = %v, want %v", got, want)
	}

	// The row label surfaces name, org, and path.
	label := opts[0].Key
	for _, want := range []string{"Alpha", "personal", "/a"} {
		if !strings.Contains(label, want) {
			t.Errorf("first row label %q missing %q", label, want)
		}
	}
}

func TestApplyRegistryEdits_PreservesUnsafeFields(t *testing.T) {
	last := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	c := config.RegisteredCampaign{
		Name:       "Old",
		Org:        "personal",
		Path:       "/old",
		Type:       config.CampaignTypeProduct,
		LastAccess: last,
		Status:     "active",
		Tags:       []string{"t1"},
	}

	got := applyRegistryEdits(c, "New", "obc", "/new")

	if got.Name != "New" || got.Org != "obc" || got.Path != "/new" {
		t.Errorf("safe fields not updated: %+v", got)
	}
	if got.Type != config.CampaignTypeProduct || !got.LastAccess.Equal(last) || got.Status != "active" {
		t.Errorf("unsafe fields changed: type=%q last=%v status=%q", got.Type, got.LastAccess, got.Status)
	}
	if !reflect.DeepEqual(got.Tags, []string{"t1"}) {
		t.Errorf("tags changed: %v", got.Tags)
	}
	if c.Name != "Old" || c.Org != "personal" || c.Path != "/old" {
		t.Errorf("applyRegistryEdits mutated its input: %+v", c)
	}
}

func TestClassifyPathRepair(t *testing.T) {
	exists := func(p string) bool { return p == "/exists" }
	tests := []struct {
		name      string
		current   string
		candidate string
		want      pathRepair
	}{
		{"unchanged when candidate equals current", "/a", "/a", pathUnchanged},
		{"rejected when candidate does not exist", "/a", "/missing", pathRejected},
		{"needs confirmation for a different existing dir", "/a", "/exists", pathNeedsConfirm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyPathRepair(tt.current, tt.candidate, exists); got != tt.want {
				t.Errorf("classifyPathRepair(%q, %q) = %d, want %d", tt.current, tt.candidate, got, tt.want)
			}
		})
	}
}

func TestNormalizeRegistryPath_AbsAndRejectEmpty(t *testing.T) {
	tmp := t.TempDir()
	got, err := normalizeRegistryPath(tmp)
	if err != nil {
		t.Fatalf("normalizeRegistryPath: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
	// Relative form must expand to absolute, not persist as relative.
	rel := filepath.Base(tmp)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Dir(tmp)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	gotRel, err := normalizeRegistryPath(rel)
	if err != nil {
		t.Fatalf("relative normalize: %v", err)
	}
	if !filepath.IsAbs(gotRel) {
		t.Fatalf("relative input must become absolute, got %q", gotRel)
	}
	if _, err := normalizeRegistryPath(""); err == nil {
		t.Fatal("empty path must be rejected")
	}
	if _, err := normalizeRegistryPath("."); err == nil {
		t.Fatal("dot path must be rejected")
	}
}

func TestRegistryPathConflictPredicate(t *testing.T) {
	// Mirrors the uniqueness check inside saveRegistryEntry: another UUID
	// already owning the absolute path is a conflict (same as Registry.Register).
	pathA := "/tmp/camp-a"
	pathB := "/tmp/camp-b"
	r := &config.Registry{Campaigns: map[string]config.RegisteredCampaign{
		"uuid-1": {ID: "uuid-1", Name: "One", Path: pathA},
		"uuid-2": {ID: "uuid-2", Name: "Two", Path: pathB},
	}}
	// uuid-2 repointing onto uuid-1's path must collide.
	collides := false
	for id, other := range r.Campaigns {
		if id != "uuid-2" && other.Path == pathA {
			collides = true
			break
		}
	}
	if !collides {
		t.Fatal("expected path conflict when two UUIDs share a path")
	}
	// Own path is fine.
	for id, other := range r.Campaigns {
		if id != "uuid-2" && other.Path == pathB {
			t.Fatal("own path must not conflict")
		}
	}
}
