package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
)

// findCmd finds a command in the root tree by path (e.g. "create", "dungeon list").
func findCmd(path ...string) *cobra.Command {
	cmd, _, err := rootCmd.Find(path)
	if err != nil || cmd == nil || cmd.Name() != path[len(path)-1] {
		return nil
	}
	return cmd
}

func flowCommandsRegistered() bool {
	cmd, _, err := rootCmd.Find([]string{"flow"})
	return err == nil && cmd != nil && cmd.Name() == "flow"
}

func questCommandsRegistered() bool {
	cmd, _, err := rootCmd.Find([]string{"quest"})
	return err == nil && cmd != nil && cmd.Name() == "quest"
}

func workitemCommandRegistered() bool {
	cmd, _, err := rootCmd.Find([]string{"workitem"})
	return err == nil && cmd != nil && cmd.Name() == "workitem"
}

func TestManifestCommand_OutputsValidJSON(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"__manifest"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("__manifest command failed: %v", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(buf.Bytes(), &manifest); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, buf.String())
	}
}

func TestManifestCommand_SchemaFields(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"__manifest"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("__manifest command failed: %v", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(buf.Bytes(), &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if manifest.Version != 2 {
		t.Errorf("expected version 2, got %d", manifest.Version)
	}
	if manifest.CLI != "camp" {
		t.Errorf("expected cli 'camp', got %q", manifest.CLI)
	}
}

func TestManifestCommand_CoversVisibleCommandTree(t *testing.T) {
	var missing []string
	walkMissingManifestAnnotations(rootCmd, "", &missing)
	if len(missing) > 0 {
		t.Fatalf("commands missing agent_allowed annotation: %v", missing)
	}
}

func walkMissingManifestAnnotations(cmd *cobra.Command, prefix string, missing *[]string) {
	for _, child := range cmd.Commands() {
		path := child.Name()
		if prefix != "" {
			path = prefix + " " + child.Name()
		}
		if skipManifestCommand(path, child) {
			continue
		}
		if _, ok := child.Annotations["agent_allowed"]; !ok {
			*missing = append(*missing, path)
		}
		walkMissingManifestAnnotations(child, path, missing)
	}
}

func TestManifestCommand_AllRestrictedCommandsPresent(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"__manifest"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("__manifest command failed: %v", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(buf.Bytes(), &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	expectedCommands := map[string]bool{
		"init":          false,
		"create":        false,
		"clone":         false,
		"switch":        false,
		"register":      false,
		"unregister":    false,
		"settings":      false,
		"settings get":  false,
		"settings set":  false,
		"shell-init":    false,
		"move":          false,
		"doctor":        false,
		"dungeon crawl": false,
		"dungeon list":  false,
		"dungeon move":  false,
		"intent crawl":  false,
		"skills link":   false,
		"skills status": false,
		"skills unlink": false,
	}
	if flowCommandsRegistered() {
		expectedCommands["flow"] = false
		expectedCommands["flow add"] = false
		expectedCommands["flow migrate"] = false
	}
	if questCommandsRegistered() {
		expectedCommands["quest archive"] = false
		expectedCommands["quest complete"] = false
		expectedCommands["quest create"] = false
		expectedCommands["quest edit"] = false
		expectedCommands["quest link"] = false
		expectedCommands["quest links"] = false
		expectedCommands["quest list"] = false
		expectedCommands["quest pause"] = false
		expectedCommands["quest rename"] = false
		expectedCommands["quest restore"] = false
		expectedCommands["quest resume"] = false
		expectedCommands["quest show"] = false
		expectedCommands["quest unlink"] = false
	}
	if workitemCommandRegistered() {
		expectedCommands["workitem"] = false
		expectedCommands["workitem create"] = false
		expectedCommands["workitem current"] = false
		expectedCommands["workitem link"] = false
		expectedCommands["workitem links"] = false
		expectedCommands["workitem priority"] = false
		expectedCommands["workitem resolve"] = false
		expectedCommands["workitem unlink"] = false
		expectedCommands["workitem commit"] = false
		expectedCommands["workitem commits"] = false
		expectedCommands["workitem priority"] = false
	}

	for _, cmd := range manifest.Commands {
		if _, ok := expectedCommands[cmd.Path]; ok {
			expectedCommands[cmd.Path] = true
		}
	}

	for path, found := range expectedCommands {
		if !found {
			t.Errorf("restricted command %q not found in manifest output", path)
		}
	}

	wantCount := countVisibleManifestCommands(rootCmd, "")
	if len(manifest.Commands) != wantCount {
		t.Errorf("expected exactly %d manifest commands, got %d", wantCount, len(manifest.Commands))
	}
}

func TestManifestCommand_AllCommandsHaveAnnotations(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"__manifest"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("__manifest command failed: %v", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(buf.Bytes(), &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	agentAllowed := map[string]bool{}
	for path := range manifestAgentAllowedReasons {
		agentAllowed[path] = true
	}

	for _, cmd := range manifest.Commands {
		if cmd.AgentAllowed && !agentAllowed[cmd.Path] {
			t.Errorf("command %q is agent_allowed=true but not in allowlist — add it or set agent_allowed=false", cmd.Path)
		}
		if !cmd.AgentAllowed && agentAllowed[cmd.Path] {
			t.Errorf("command %q should be agent_allowed=true", cmd.Path)
		}
		if cmd.Reason == "" {
			t.Errorf("command %q has empty reason", cmd.Path)
		}
	}
}

func TestManifestCommand_InteractiveFlags(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"__manifest"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("__manifest command failed: %v", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(buf.Bytes(), &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	interactiveCommands := map[string]bool{
		"init":          true,
		"create":        true,
		"intent add":    true,
		"switch":        true,
		"settings":      true,
		"move":          true,
		"dungeon crawl": true,
		"intent crawl":  true,
	}
	if questCommandsRegistered() {
		interactiveCommands["quest edit"] = true
	}

	nonInteractiveCommands := map[string]bool{
		"clone":         true,
		"register":      true,
		"unregister":    true,
		"settings get":  true,
		"settings set":  true,
		"shell-init":    true,
		"doctor":        true,
		"dungeon list":  true,
		"dungeon move":  true,
		"skills link":   true,
		"skills status": true,
		"skills unlink": true,
	}
	if flowCommandsRegistered() {
		nonInteractiveCommands["flow add"] = true
		nonInteractiveCommands["flow migrate"] = true
	}
	if questCommandsRegistered() {
		nonInteractiveCommands["quest archive"] = true
		nonInteractiveCommands["quest complete"] = true
		nonInteractiveCommands["quest create"] = true
		nonInteractiveCommands["quest link"] = true
		nonInteractiveCommands["quest links"] = true
		nonInteractiveCommands["quest list"] = true
		nonInteractiveCommands["quest pause"] = true
		nonInteractiveCommands["quest rename"] = true
		nonInteractiveCommands["quest restore"] = true
		nonInteractiveCommands["quest resume"] = true
		nonInteractiveCommands["quest show"] = true
		nonInteractiveCommands["quest unlink"] = true
	}
	if workitemCommandRegistered() {
		nonInteractiveCommands["workitem"] = true
		nonInteractiveCommands["workitem create"] = true
		nonInteractiveCommands["workitem current"] = true
		nonInteractiveCommands["workitem link"] = true
		nonInteractiveCommands["workitem links"] = true
		nonInteractiveCommands["workitem priority"] = true
		nonInteractiveCommands["workitem resolve"] = true
		nonInteractiveCommands["workitem unlink"] = true
		nonInteractiveCommands["workitem commit"] = true
		nonInteractiveCommands["workitem commits"] = true
	}

	cmdMap := make(map[string]CommandEntry)
	for _, cmd := range manifest.Commands {
		cmdMap[cmd.Path] = cmd
	}

	for path := range interactiveCommands {
		cmd, ok := cmdMap[path]
		if !ok {
			t.Errorf("interactive command %q not found in manifest", path)
			continue
		}
		if !cmd.Interactive {
			t.Errorf("command %q should be marked interactive but is not", path)
		}
	}

	for path := range nonInteractiveCommands {
		cmd, ok := cmdMap[path]
		if !ok {
			continue
		}
		if cmd.Interactive {
			t.Errorf("command %q should NOT be marked interactive but is", path)
		}
	}
}

func countVisibleManifestCommands(cmd *cobra.Command, prefix string) int {
	var count int
	for _, child := range cmd.Commands() {
		path := child.Name()
		if prefix != "" {
			path = prefix + " " + child.Name()
		}
		if skipManifestCommand(path, child) {
			continue
		}
		count++
		count += countVisibleManifestCommands(child, path)
	}
	return count
}

// TestCampCreate_ManifestAnnotations asserts the registration, group, annotations,
// supported flags, and absent flags for the 'camp create' command.
func TestCampCreate_ManifestAnnotations(t *testing.T) {
	cmd := findCmd("create")
	if cmd == nil {
		t.Fatal("camp create command not registered")
	}

	// Group
	if cmd.GroupID != "setup" {
		t.Errorf("createCmd GroupID = %q, want %q", cmd.GroupID, "setup")
	}

	// Annotations
	if v := cmd.Annotations["agent_allowed"]; v != "true" {
		t.Errorf("annotation agent_allowed = %q, want %q", v, "true")
	}
	if v := cmd.Annotations["agent_reason"]; v == "" {
		t.Error("annotation agent_reason is empty")
	}
	if v := cmd.Annotations["interactive"]; v != "true" {
		t.Errorf("annotation interactive = %q, want %q", v, "true")
	}
	wantReason := "Non-interactive with -d and -m; interactive fallback otherwise"
	if v := cmd.Annotations["agent_reason"]; v != wantReason {
		t.Errorf("annotation agent_reason = %q, want %q", v, wantReason)
	}

	// Supported flags
	supportedFlags := []string{
		"name", "type", "description", "mission",
		"no-git", "dry-run", "path",
	}
	for _, flag := range supportedFlags {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("camp create missing expected flag --%s", flag)
		}
	}

	// Absent flags: create must NOT support init-only controls.
	absentFlags := []string{"force", "repair", "no-register", "yes"}
	for _, flag := range absentFlags {
		if cmd.Flags().Lookup(flag) != nil {
			t.Errorf("camp create should NOT have flag --%s", flag)
		}
	}
}
