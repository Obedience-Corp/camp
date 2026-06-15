package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/spf13/cobra"
)

func TestConceptsCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "concepts" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("concepts command not registered on root")
	}
}

func TestConceptsGroupID(t *testing.T) {
	if conceptsCmd.GroupID != "campaign" {
		t.Errorf("GroupID = %q, want %q", conceptsCmd.GroupID, "campaign")
	}
}

func TestConceptsAlias(t *testing.T) {
	aliases := conceptsCmd.Aliases
	found := false
	for _, a := range aliases {
		if a == "concept" {
			found = true
			break
		}
	}
	if !found {
		t.Error("concepts command missing 'concept' alias")
	}
}

func TestConceptsOutsideCampaignReturnsError(t *testing.T) {
	chdirForTest(t, t.TempDir())

	cmd := newConceptsCommand()
	cmd.SetContext(context.Background())
	err := runConcepts(cmd, nil)
	if err == nil {
		t.Fatal("runConcepts() error = nil, want non-zero error")
	}
	if !errors.Is(err, campaign.ErrNotInCampaign) {
		t.Fatalf("runConcepts() error = %v, want ErrNotInCampaign", err)
	}
}

func TestConceptsJSONOutsideCampaignErrorEnvelope(t *testing.T) {
	chdirForTest(t, t.TempDir())

	stdout, stderr, err := executeConceptsTestCommand(t, newConceptsCommand(), "--json")
	if err == nil {
		t.Fatal("concepts --json outside campaign error = nil, want non-zero command error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty on JSON error", stdout)
	}

	var cmdErr *camperrors.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error = %T %v, want *CommandError", err, err)
	}
	if cmdErr.ExitCode == 0 {
		t.Fatal("CommandError.ExitCode = 0, want non-zero")
	}

	var envelope jsoncontract.ErrorEnvelope
	if err := json.Unmarshal([]byte(stderr), &envelope); err != nil {
		t.Fatalf("stderr is not error envelope JSON: %v\nraw=%s", err, stderr)
	}
	if envelope.SchemaVersion != ConceptsJSONVersion {
		t.Fatalf("schema_version = %q, want %q", envelope.SchemaVersion, ConceptsJSONVersion)
	}
	if envelope.Error.ExitCode == 0 {
		t.Fatal("error.exit_code = 0, want non-zero")
	}
	if envelope.Error.Hint != "run 'camp init' to create a new campaign" {
		t.Fatalf("error.hint = %q", envelope.Error.Hint)
	}
}

func TestConceptsJSONPayload(t *testing.T) {
	root := setupConceptsJSONCampaign(t)
	chdirForTest(t, root)

	stdout, stderr, err := executeConceptsTestCommand(t, newConceptsCommand(), "--json")
	if err != nil {
		t.Fatalf("concepts --json error = %v\nstderr=%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}

	var payload conceptsPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout is not concepts JSON: %v\nraw=%s", err, stdout)
	}
	if payload.SchemaVersion != ConceptsJSONVersion {
		t.Fatalf("schema_version = %q, want %q", payload.SchemaVersion, ConceptsJSONVersion)
	}
	if payload.GeneratedAt.IsZero() {
		t.Fatal("generated_at is zero")
	}
	expectedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	expectedRoot, err = filepath.Abs(expectedRoot)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}
	if payload.CampaignRoot != expectedRoot {
		t.Fatalf("campaign_root = %q, want %q", payload.CampaignRoot, expectedRoot)
	}
	if len(payload.Concepts) != 1 {
		t.Fatalf("len(concepts) = %d, want 1", len(payload.Concepts))
	}

	got := payload.Concepts[0]
	if got.Name != "p" {
		t.Fatalf("concept.name = %q, want %q", got.Name, "p")
	}
	if got.Path != "projects" {
		t.Fatalf("concept.path = %q, want %q", got.Path, "projects")
	}
	if got.Description != "Project directories" {
		t.Fatalf("concept.description = %q", got.Description)
	}
	if got.MaxDepth == nil || *got.MaxDepth != 1 {
		t.Fatalf("concept.max_depth = %v, want 1", got.MaxDepth)
	}
	if !got.HasItems {
		t.Fatal("concept.has_items = false, want true")
	}
	if len(got.Ignore) != 1 || got.Ignore[0] != "ignore-me" {
		t.Fatalf("concept.ignore = %#v", got.Ignore)
	}
}

func executeConceptsTestCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}

func setupConceptsJSONCampaign(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "projects", "app"), 0755); err != nil {
		t.Fatalf("mkdir concept item: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "projects", "ignore-me"), 0755); err != nil {
		t.Fatalf("mkdir ignored concept item: %v", err)
	}

	depth := 1
	cfg := &config.CampaignConfig{
		ID:        "concepts-json-test",
		Name:      "concepts-json-test",
		Type:      config.CampaignTypeProduct,
		CreatedAt: time.Now(),
		ConceptList: []config.ConceptEntry{
			{
				Name:        "p",
				Path:        "projects",
				Description: "Project directories",
				Depth:       &depth,
				Ignore:      []string{"ignore-me"},
			},
		},
	}
	if err := config.SaveCampaignConfig(context.Background(), root, cfg); err != nil {
		t.Fatalf("save campaign config: %v", err)
	}
	return root
}
