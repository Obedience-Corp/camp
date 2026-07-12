package main

import (
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
