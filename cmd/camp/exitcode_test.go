package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	intclone "github.com/Obedience-Corp/camp/internal/clone"
	"github.com/Obedience-Corp/camp/internal/doctor"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intsync "github.com/Obedience-Corp/camp/internal/sync"
	"github.com/spf13/cobra"
)

func TestExitCodeCommandErrors(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T) error
		want int
	}{
		{
			name: "doctor findings return runtime failure",
			run:  runDoctorExitCodeFailure,
			want: doctor.ExitFailures,
		},
		{
			name: "sync preflight failure returns runtime failure",
			run:  runSyncExitCodePreflightFailure,
			want: intsync.ExitPreflightFailed,
		},
		{
			name: "clone partial result returns partial code",
			run:  runCloneExitCodePartialFailure,
			want: intclone.ExitPartialSuccess,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(t)
			var cmdErr *camperrors.CommandError
			if !errors.As(err, &cmdErr) {
				t.Fatalf("error = %T %v, want *CommandError", err, err)
			}
			if cmdErr.ExitCode != tt.want {
				t.Fatalf("exit code = %d, want %d", cmdErr.ExitCode, tt.want)
			}
		})
	}
}

func runDoctorExitCodeFailure(t *testing.T) error {
	t.Helper()

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
	_, err := captureDoctorJSONStdout(t, func() error {
		return cmd.ExecuteContext(context.Background())
	})
	return err
}

func runSyncExitCodePreflightFailure(t *testing.T) error {
	t.Helper()

	root := setupSyncExitCodeCampaign(t)
	installSyncExitCodeFakeGit(t)
	t.Setenv(campaign.EnvCampaignRoot, root)
	t.Setenv(campaign.EnvCacheDisable, "1")

	oldOpts := syncOpts
	syncOpts = struct {
		dryRun   bool
		force    bool
		verbose  bool
		parallel int
		noFetch  bool
		json     bool
		from     string
	}{}
	t.Cleanup(func() { syncOpts = oldOpts })

	cmd := newSyncExitCodeTestCommand()
	cmd.SetArgs([]string{"--json"})
	_, err := captureDoctorJSONStdout(t, func() error {
		return cmd.ExecuteContext(context.Background())
	})
	return err
}

func runCloneExitCodePartialFailure(t *testing.T) error {
	t.Helper()

	return determineCloneExitCode(&intclone.CloneResult{
		Success: false,
		Submodules: []intclone.SubmoduleResult{
			{Name: "alpha", Success: true},
			{Name: "beta", Success: false},
		},
	})
}

func newSyncExitCodeTestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "sync [submodule...]",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE:          runSync,
	}
	cmd.Flags().BoolVarP(&syncOpts.dryRun, "dry-run", "n", false, "")
	cmd.Flags().BoolVarP(&syncOpts.force, "force", "f", false, "")
	cmd.Flags().BoolVarP(&syncOpts.verbose, "verbose", "v", false, "")
	cmd.Flags().IntVarP(&syncOpts.parallel, "parallel", "p", 4, "")
	cmd.Flags().BoolVar(&syncOpts.noFetch, "no-fetch", false, "")
	cmd.Flags().BoolVar(&syncOpts.json, "json", false, "")
	return cmd
}

func setupSyncExitCodeCampaign(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0755); err != nil {
		t.Fatalf("mkdir .campaign: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "projects", "alpha"), 0755); err != nil {
		t.Fatalf("mkdir submodule: %v", err)
	}
	config := "id: sync-exit-code\nname: sync-exit-code\ntype: product\n"
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"), []byte(config), 0644); err != nil {
		t.Fatalf("write campaign config: %v", err)
	}
	return root
}

func installSyncExitCodeFakeGit(t *testing.T) {
	t.Helper()

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir fake git dir: %v", err)
	}
	script := `#!/bin/sh
repo=""
if [ "$1" = "-C" ]; then
	repo="$2"
	shift 2
fi

if [ "$1" = "config" ] && [ "$2" = "-f" ] && [ "$3" = ".gitmodules" ] && [ "$4" = "--get-regexp" ]; then
	printf 'submodule.alpha.path projects/alpha\n'
	exit 0
fi

if [ "$1" = "config" ] && [ "$2" = "-f" ] && [ "$3" = ".gitmodules" ]; then
	printf 'https://example.com/alpha.git\n'
	exit 0
fi

if [ "$1" = "config" ] && [ "$2" = "submodule.projects/alpha.url" ]; then
	printf 'https://example.com/alpha.git\n'
	exit 0
fi

if [ "$1" = "diff" ] && [ "$2" = "--cached" ] && [ "$3" = "--quiet" ]; then
	exit 0
fi

if [ "$1" = "diff" ] && [ "$2" = "--quiet" ]; then
	exit 1
fi

if [ "$1" = "status" ] && [ "$2" = "--porcelain" ]; then
	printf ' M file.txt\n'
	exit 0
fi

if [ "$1" = "log" ] && [ "$2" = "--oneline" ]; then
	exit 1
fi

if [ "$1" = "symbolic-ref" ] && [ "$2" = "-q" ] && [ "$3" = "HEAD" ]; then
	printf 'refs/heads/main\n'
	exit 0
fi

printf 'unexpected fake git invocation: repo=%s args=%s\n' "$repo" "$*" >&2
exit 1
`
	gitPath := filepath.Join(binDir, "git")
	if err := os.WriteFile(gitPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
