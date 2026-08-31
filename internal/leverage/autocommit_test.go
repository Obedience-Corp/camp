package leverage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/git"
)

func newFakeCampaign(t *testing.T, withDataDir bool) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if withDataDir {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(DataDir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestAutocommit_SkipsAndFailures(t *testing.T) {
	boom := errors.New("git exploded")

	tests := []struct {
		name       string
		root       func(t *testing.T) string
		commitErr  error
		wantStatus CommitStatus
		wantErr    bool
		wantReason string
	}{
		{
			name:       "no campaign root",
			root:       func(*testing.T) string { return "" },
			wantStatus: CommitSkipped,
			wantReason: "no campaign root",
		},
		{
			name:       "campaign root is not a git repo",
			root:       func(t *testing.T) string { return t.TempDir() },
			wantStatus: CommitSkipped,
			wantReason: "not a git repository",
		},
		{
			name:       "leverage data dir missing",
			root:       func(t *testing.T) string { return newFakeCampaign(t, false) },
			wantStatus: CommitSkipped,
			wantReason: "does not exist",
		},
		{
			name:       "git reports nothing to commit",
			root:       func(t *testing.T) string { return newFakeCampaign(t, true) },
			commitErr:  git.ErrNoChanges,
			wantStatus: CommitUnchanged,
			wantReason: "no leverage changes to commit",
		},
		{
			name:      "git failure surfaces to the caller",
			root:      func(t *testing.T) string { return newFakeCampaign(t, true) },
			commitErr: boom,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			commitFn := func(context.Context, string, []string, *git.CommitOptions) error {
				return tc.commitErr
			}

			res, err := Autocommit(context.Background(), CommitRequest{
				CampaignRoot: tc.root(t),
				Subject:      "update scores",
			}, commitFn)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if !errors.Is(err, boom) {
					t.Fatalf("error should wrap the git failure, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Status != tc.wantStatus {
				t.Errorf("status = %v, want %v", res.Status, tc.wantStatus)
			}
			if !strings.Contains(res.Reason, tc.wantReason) {
				t.Errorf("reason = %q, want it to contain %q", res.Reason, tc.wantReason)
			}
		})
	}
}

func TestAutocommit_HonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	_, err := Autocommit(ctx, CommitRequest{
		CampaignRoot: newFakeCampaign(t, true),
		Subject:      "update scores",
	}, func(context.Context, string, []string, *git.CommitOptions) error {
		called = true
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if called {
		t.Error("a canceled context must not reach git")
	}
}

func TestAutocommit_StagesOnlyTheLeverageDataDir(t *testing.T) {
	root := newFakeCampaign(t, true)

	var gotPaths []string
	var gotMessage string
	res, err := Autocommit(context.Background(), CommitRequest{
		CampaignRoot: root,
		CampaignName: "obey-campaign",
		CampaignID:   "8deed8b4",
		Subject:      "snapshot",
		Report:       "Saved 2 snapshots.",
	}, func(_ context.Context, repoPath string, paths []string, opts *git.CommitOptions) error {
		if repoPath != root {
			t.Errorf("repoPath = %q, want %q", repoPath, root)
		}
		gotPaths = paths
		gotMessage = opts.Message
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != CommitCreated {
		t.Fatalf("status = %v, want CommitCreated", res.Status)
	}

	if len(gotPaths) != 1 || gotPaths[0] != DataDir {
		t.Errorf("staged paths = %v, want exactly [%s]", gotPaths, DataDir)
	}
	subject, body, _ := strings.Cut(gotMessage, "\n\n")
	if !strings.Contains(subject, "leverage: snapshot") {
		t.Errorf("subject = %q, want it to contain %q", subject, "leverage: snapshot")
	}
	if !strings.Contains(subject, "8deed8b4") {
		t.Errorf("subject = %q, want it to carry the campaign tag", subject)
	}
	if body != "Saved 2 snapshots." {
		t.Errorf("body = %q, want the rendered report", body)
	}
}

func TestCommitMessage_OmitsBodyWhenReportIsEmpty(t *testing.T) {
	msg := CommitMessage(CommitRequest{CampaignID: "abc12345", Subject: "reset"})
	if strings.Contains(msg, "\n") {
		t.Errorf("message = %q, want a subject line only", msg)
	}
	if !strings.HasSuffix(msg, "leverage: reset") {
		t.Errorf("message = %q, want it to end with the subject", msg)
	}
}

func TestAutocommitEnabled(t *testing.T) {
	on, off := true, false

	tests := []struct {
		name string
		cfg  *LeverageConfig
		want bool
	}{
		{name: "nil config defaults on", cfg: nil, want: true},
		{name: "unset defaults on", cfg: &LeverageConfig{}, want: true},
		{name: "explicit false is off", cfg: &LeverageConfig{Autocommit: &off}, want: false},
		{name: "explicit true is on", cfg: &LeverageConfig{Autocommit: &on}, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.AutocommitEnabled(); got != tc.want {
				t.Errorf("AutocommitEnabled() = %t, want %t", got, tc.want)
			}
		})
	}
}
