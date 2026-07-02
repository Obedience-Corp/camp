package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/doctor"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/spf13/cobra"
)

func TestDoctorJSONReturnsFailureCodeAfterValidJSON(t *testing.T) {
	root := setupDoctorJSONLockCampaign(t)
	installDoctorJSONFakeFuser(t)
	t.Setenv(campaign.EnvCampaignRoot, root)
	t.Setenv(campaign.EnvCacheDisable, "1")

	oldOpts := doctorOpts
	doctorOpts = struct {
		fix            bool
		verbose        bool
		jsonOutput     bool
		submodulesOnly bool
		checks         []string
	}{}
	t.Cleanup(func() { doctorOpts = oldOpts })

	cmd := newDoctorJSONTestCommand()
	cmd.SetArgs([]string{"--json", "--check", "lock"})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	stdout, err := captureDoctorJSONStdout(t, func() error {
		return cmd.ExecuteContext(context.Background())
	})
	if err == nil {
		t.Fatal("doctor --json error = nil, want non-zero command error")
	}

	var cmdErr *camperrors.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("doctor --json error = %T %v, want *CommandError", err, err)
	}
	if cmdErr.ExitCode != doctor.ExitFailures {
		t.Fatalf("doctor --json exit code = %d, want %d", cmdErr.ExitCode, doctor.ExitFailures)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("doctor --json stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if len(payload) == 0 {
		t.Fatal("doctor --json emitted an empty JSON object")
	}

	var envelope jsoncontract.ErrorEnvelope
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("doctor --json stderr is not a JSON error envelope: %v\n%s", err, stderr.String())
	}
	if envelope.SchemaVersion != DoctorJSONVersion {
		t.Fatalf("error schema_version = %q, want %q", envelope.SchemaVersion, DoctorJSONVersion)
	}
	if envelope.Error.ExitCode != doctor.ExitFailures {
		t.Fatalf("error exit_code = %d, want %d", envelope.Error.ExitCode, doctor.ExitFailures)
	}
}

func newDoctorJSONTestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "doctor",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          jsoncontract.RunE(DoctorJSONVersion, func() bool { return doctorOpts.jsonOutput }, runDoctor),
	}
	cmd.Flags().BoolVarP(&doctorOpts.fix, "fix", "f", false, "")
	cmd.Flags().BoolVarP(&doctorOpts.verbose, "verbose", "v", false, "")
	cmd.Flags().BoolVar(&doctorOpts.jsonOutput, "json", false, "")
	cmd.Flags().BoolVar(&doctorOpts.submodulesOnly, "submodules-only", false, "")
	cmd.Flags().StringSliceVarP(&doctorOpts.checks, "check", "c", nil, "")
	return cmd
}

func setupDoctorJSONLockCampaign(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0755); err != nil {
		t.Fatalf("mkdir campaign dir: %v", err)
	}
	config := "id: test-doctor-json\nname: test-doctor-json\ntype: product\n"
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"), []byte(config), 0644); err != nil {
		t.Fatalf("write campaign config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatalf("mkdir git dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "index.lock"), []byte("stale\n"), 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	return root
}

func installDoctorJSONFakeFuser(t *testing.T) {
	t.Helper()

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir fake fuser dir: %v", err)
	}
	fuserPath := filepath.Join(binDir, "fuser")
	if err := os.WriteFile(fuserPath, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatalf("write fake fuser: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestOutputDoctorJSONUsesSnakeCaseKeysAndSchemaVersion(t *testing.T) {
	result := &doctor.DoctorResult{
		Success: false,
		Passed:  1,
		Warned:  1,
		Failed:  1,
		Issues: []doctor.Issue{
			{Severity: doctor.SeverityError, CheckID: "url", Description: "mismatch"},
		},
		CheckResults: map[string]bool{"url": false},
	}

	stdout, err := captureDoctorJSONStdout(t, func() error {
		return outputDoctorJSON(result)
	})
	if err != nil {
		t.Fatalf("outputDoctorJSON: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("doctor JSON output is not valid JSON: %v\n%s", err, stdout)
	}

	for _, key := range []string{"schema_version", "success", "passed", "warned", "failed", "issues", "fixed", "check_results"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("doctor JSON missing expected snake_case key %q, got: %s", key, stdout)
		}
	}
	for _, key := range []string{"Success", "Passed", "Warned", "Failed", "Issues", "Fixed", "CheckResults"} {
		if _, ok := raw[key]; ok {
			t.Errorf("doctor JSON still emits PascalCase key %q, got: %s", key, stdout)
		}
	}

	var schemaVersion string
	if err := json.Unmarshal(raw["schema_version"], &schemaVersion); err != nil {
		t.Fatalf("schema_version is not a string: %v", err)
	}
	if schemaVersion != DoctorJSONVersion {
		t.Errorf("schema_version = %q, want %q", schemaVersion, DoctorJSONVersion)
	}

	var issues []map[string]json.RawMessage
	if err := json.Unmarshal(raw["issues"], &issues); err != nil {
		t.Fatalf("issues is not an array: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues length = %d, want 1", len(issues))
	}
	for _, key := range []string{"severity", "check_id", "description", "auto_fixable"} {
		if _, ok := issues[0][key]; !ok {
			t.Errorf("issue missing expected snake_case key %q, got: %v", key, issues[0])
		}
	}
	var severity string
	if err := json.Unmarshal(issues[0]["severity"], &severity); err != nil {
		t.Fatalf("severity is not a string: %v", err)
	}
	if severity != "error" {
		t.Errorf("severity = %q, want %q", severity, "error")
	}
}

func TestOutputDoctorJSONEmitsEmptyArraysNotNull(t *testing.T) {
	result := &doctor.DoctorResult{Success: true, CheckResults: map[string]bool{}}

	stdout, err := captureDoctorJSONStdout(t, func() error {
		return outputDoctorJSON(result)
	})
	if err != nil {
		t.Fatalf("outputDoctorJSON: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("doctor JSON output is not valid JSON: %v\n%s", err, stdout)
	}
	for _, key := range []string{"issues", "fixed"} {
		got := string(raw[key])
		if got != "[]" {
			t.Errorf("%s = %s, want empty array []", key, got)
		}
	}
	if result.Issues != nil {
		t.Errorf("outputDoctorJSON mutated result.Issues to non-nil: %#v", result.Issues)
	}
	if result.Fixed != nil {
		t.Errorf("outputDoctorJSON mutated result.Fixed to non-nil: %#v", result.Fixed)
	}
}

func captureDoctorJSONStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	os.Stdout = w

	runErr := fn()
	_ = w.Close()
	os.Stdout = oldStdout

	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	return string(out), runErr
}
