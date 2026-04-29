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

	if manifest.Version != 1 {
		t.Errorf("expected version 1, got %d", manifest.Version)
	}
	if manifest.CLI != "camp" {
		t.Errorf("expected cli 'camp', got %q", manifest.CLI)
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
		"shell-init":    false,
		"move":          false,
		"doctor":        false,
		"dungeon crawl": false,
		"dungeon list":  false,
		"dungeon move":  false,
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

	wantCount := 16
	if flowCommandsRegistered() {
		wantCount = 19
	}
	if questCommandsRegistered() {
		wantCount += 13
	}
	if workitemCommandRegistered() {
		wantCount += 1
	}
	if len(manifest.Commands) != wantCount {
		t.Errorf("expected exactly %d restricted commands, got %d", wantCount, len(manifest.Commands))
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

	// Commands that are explicitly agent-allowed (have non-interactive input modes)
	agentAllowed := map[string]bool{
		"create":        true,
		"dungeon list":  true,
		"dungeon move":  true,
		"switch":        true,
		"skills link":   true,
		"skills status": true,
		"skills unlink": true,
	}
	if flowCommandsRegistered() {
		agentAllowed["flow add"] = true
	}
	if questCommandsRegistered() {
		agentAllowed["quest archive"] = true
		agentAllowed["quest complete"] = true
		agentAllowed["quest create"] = true
		agentAllowed["quest link"] = true
		agentAllowed["quest links"] = true
		agentAllowed["quest list"] = true
		agentAllowed["quest pause"] = true
		agentAllowed["quest rename"] = true
		agentAllowed["quest restore"] = true
		agentAllowed["quest resume"] = true
		agentAllowed["quest show"] = true
		agentAllowed["quest unlink"] = true
	}
	if workitemCommandRegistered() {
		agentAllowed["workitem"] = true
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
		"switch":        true,
		"settings":      true,
		"move":          true,
		"dungeon crawl": true,
	}
	if questCommandsRegistered() {
		interactiveCommands["quest edit"] = true
	}

	nonInteractiveCommands := map[string]bool{
		"clone":         true,
		"register":      true,
		"unregister":    true,
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
	absentFlags := []string{"force", "repair", "no-register", "yes", "skip-fest"}
	for _, flag := range absentFlags {
		if cmd.Flags().Lookup(flag) != nil {
			t.Errorf("camp create should NOT have flag --%s", flag)
		}
	}
}
