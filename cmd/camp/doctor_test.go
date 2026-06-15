package main

import (
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
	"github.com/spf13/cobra"
)

func TestDoctorJSONReturnsFailureCodeAfterValidJSON(t *testing.T) {
	root := setupDoctorJSONLockCampaign(t)
	installDoctorJSONFakeFuser(t)
	t.Setenv(campaign.EnvCampaignRoot, root)
	campaign.ClearCache()
	t.Cleanup(campaign.ClearCache)

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
}

func newDoctorJSONTestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "doctor",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          runDoctor,
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
