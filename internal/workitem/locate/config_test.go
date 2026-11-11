package locate

import (
	"path/filepath"
	"strings"
	"testing"
)

// noGlobalConfig holds no config.json, so dungeon_hidden takes its default.
const noGlobalConfig = "no-global-config"

// pinGlobalConfig points the global config at testdata/<name>, read-only.
func pinGlobalConfig(t *testing.T, name string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("resolving testdata/%s: %v", name, err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// A type folder with no dungeon yet must take the camp's spelling. With no
// dungeon anywhere in the camp, that falls through to dungeon_hidden.
func TestDetectFromCwd_NewTypeDungeonFollowsSetting(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{name: "default is hidden", config: noGlobalConfig, want: "/campaign/workflow/content/.dungeon"},
		{name: "explicit false is visible", config: "visible-config", want: "/campaign/workflow/content/dungeon"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pinGlobalConfig(t, tc.config)
			got, err := DetectFromCwd("/campaign", "/campaign/workflow/content/article")
			if err != nil {
				t.Fatalf("DetectFromCwd: %v", err)
			}
			if filepath.ToSlash(got.DungeonPath) != tc.want {
				t.Errorf("DungeonPath=%q want %q", got.DungeonPath, tc.want)
			}
		})
	}
}

// An unreadable setting is an error naming the file, never a silent fallback
// to the visible spelling.
func TestDetectFromCwd_MalformedGlobalConfigNamesFile(t *testing.T) {
	dir := pinGlobalConfig(t, "malformed-config")
	_, err := DetectFromCwd("/campaign", "/campaign/workflow/content/article")
	if err == nil {
		t.Fatal("DetectFromCwd succeeded with a malformed global config")
	}
	want := filepath.Join(dir, "obey", "campaign", "config.json")
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name %s", err, want)
	}
}
