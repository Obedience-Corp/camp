//go:build container_fs

package explore

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/creack/pty"

	"github.com/Obedience-Corp/camp/internal/config"
	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

func TestExplorePipedInputJSON(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Mkdir(filepath.Join(root, ".campaign"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"), []byte("id: fixture\nname: fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	_ = writer.Close()
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = input, slave
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	if exploreTerminal() {
		t.Fatal("pipe stdin selected interactive mode")
	}
	var out bytes.Buffer
	cmd := Cmd
	cmd.SetOut(&out)
	cmd.SetContext(context.Background())
	defer cmd.SetOut(nil)
	if err := runDungeonExplore(cmd, nil); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("pipe did not emit JSON: %s (%v)", out.String(), err)
	}
	if result["schema_version"] != intexplore.SchemaVersion {
		t.Fatalf("wrong result: %+v", result)
	}
}

func TestExploreJSONDoesNotWriteConfig(t *testing.T) {
	t.Setenv("CAMP_ROOT", "")
	root := t.TempDir()
	t.Chdir(root)
	campaignDir := filepath.Join(root, ".campaign")
	if err := os.Mkdir(campaignDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(campaignDir, "campaign.yaml")
	original := []byte("name: fixture\n")
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd := Cmd
	cmd.SetOut(&out)
	cmd.SetContext(context.Background())
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.SetOut(nil)
		_ = cmd.Flags().Set("json", "false")
	})
	if err := runDungeonExplore(cmd, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("campaign.yaml changed:\n%s", got)
	}
	jumps := filepath.Join(campaignDir, "settings", "jumps.yaml")
	if _, err := os.Stat(jumps); !os.IsNotExist(err) {
		t.Fatalf("explore created jumps.yaml: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("--json did not emit JSON: %s (%v)", out.String(), err)
	}

	// The terminal branch reads the configured theme, and that read creates
	// nothing either. With no global config there is still none afterwards.
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	if got := exploreTheme(context.Background()); got != theme.ThemeAdaptive {
		t.Fatalf("theme with nothing configured = %q", got)
	}
	if _, err := os.Stat(configHome); !os.IsNotExist(err) {
		t.Fatalf("reading the theme created the config home: %v", err)
	}
	globalPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	global := []byte(`{"tui":{"theme":"light"}}` + "\n")
	if err := os.WriteFile(globalPath, global, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := exploreTheme(context.Background()); got != theme.ThemeLight {
		t.Fatalf("theme with light configured = %q", got)
	}
	// The camp's own override wins over the global setting.
	if err := config.SaveLocalSettings(context.Background(), root, &config.LocalSettings{ThemeOverride: config.ThemeNameDark}); err != nil {
		t.Fatal(err)
	}
	local, err := os.ReadFile(config.LocalSettingsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if got := exploreTheme(context.Background()); got != theme.ThemeDark {
		t.Fatalf("theme with a dark override in the camp = %q", got)
	}
	for path, want := range map[string][]byte{globalPath: global, config.LocalSettingsPath(root): local, configPath: original} {
		if got, err := os.ReadFile(path); err != nil || string(got) != string(want) {
			t.Fatalf("reading the theme changed %s: %q, %v", path, got, err)
		}
	}
	if entries, err := os.ReadDir(filepath.Dir(globalPath)); err != nil || len(entries) != 1 {
		t.Fatalf("reading the theme left %d entries beside the global config: %v", len(entries), err)
	}

	// A config that cannot be loaded must still leave the tree untouched.
	broken := []byte("description: missing name\n")
	if err := os.WriteFile(configPath, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runDungeonExplore(cmd, nil); err == nil {
		t.Fatal("missing camp name was accepted")
	}
	got, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(broken) {
		t.Fatalf("failed explore rewrote campaign.yaml:\n%s", got)
	}
	if _, err := os.Stat(jumps); !os.IsNotExist(err) {
		t.Fatalf("failed explore created jumps.yaml: %v", err)
	}
}
