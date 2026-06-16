package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"
)

func TestApplyCampaignsDirCandidate(t *testing.T) {
	t.Run("trims and applies value", func(t *testing.T) {
		cfg := config.DefaultGlobalConfig()

		if err := applyCampaignsDirCandidate(&cfg, "  /tmp/campaigns  "); err != nil {
			t.Fatalf("applyCampaignsDirCandidate() unexpected error: %v", err)
		}
		if got := cfg.CampaignsDir; got != "/tmp/campaigns" {
			t.Fatalf("CampaignsDir = %q, want %q", got, "/tmp/campaigns")
		}
	})

	t.Run("empty clears to default sentinel", func(t *testing.T) {
		cfg := config.DefaultGlobalConfig()
		cfg.CampaignsDir = "/tmp/campaigns"

		if err := applyCampaignsDirCandidate(&cfg, "   "); err != nil {
			t.Fatalf("applyCampaignsDirCandidate() unexpected error: %v", err)
		}
		if got := cfg.CampaignsDir; got != "" {
			t.Fatalf("CampaignsDir = %q, want empty", got)
		}
	})
}

func TestApplyCampaignsDirCandidate_InvalidDoesNotMutate(t *testing.T) {
	cfg := config.DefaultGlobalConfig()
	cfg.CampaignsDir = "/tmp/original"

	if err := applyCampaignsDirCandidate(&cfg, "/tmp/bad\x00path"); err == nil {
		t.Fatal("applyCampaignsDirCandidate() expected validation error, got nil")
	}
	if got := cfg.CampaignsDir; got != "/tmp/original" {
		t.Fatalf("CampaignsDir mutated on invalid input: got %q, want %q", got, "/tmp/original")
	}
}

func TestRunSettings_NonTTY(t *testing.T) {
	withNonTTYStdio(t)

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runSettings(cmd, nil)
	if err == nil {
		t.Fatal("runSettings() expected non-TTY error, got nil")
	}
	if !errors.Is(err, camperrors.ErrInvalidInput) {
		t.Fatalf("runSettings() error = %v, want ErrInvalidInput", err)
	}
	if !strings.Contains(err.Error(), "settings requires an interactive terminal") {
		t.Fatalf("runSettings() error missing terminal guidance: %v", err)
	}
}

func withNonTTYStdio(t *testing.T) {
	t.Helper()

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		t.Fatal(err)
	}

	oldStdin, oldStdout := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = stdinR, stdoutW
	t.Cleanup(func() {
		os.Stdin, os.Stdout = oldStdin, oldStdout
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
	})
}
