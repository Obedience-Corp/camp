package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

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
		"clone":         false,
		"switch":        false,
		"register":      false,
		"unregister":    false,
		"settings":      false,
		"shell-init":    false,
		"move":          false,
		"doctor":        false,
		"dungeon crawl": false,
		"flow":          false,
		"flow add":      false,
		"flow migrate":  false,
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

	if len(manifest.Commands) != 13 {
		t.Errorf("expected exactly 13 restricted commands, got %d", len(manifest.Commands))
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
		"flow add": true,
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
		"switch":        true,
		"settings":      true,
		"move":          true,
		"dungeon crawl": true,
	}

	nonInteractiveCommands := map[string]bool{
		"clone":        true,
		"register":     true,
		"unregister":   true,
		"shell-init":   true,
		"doctor":       true,
		"flow add":     true,
		"flow migrate": true,
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
