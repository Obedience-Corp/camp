package intent

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	intentcore "github.com/Obedience-Corp/camp/internal/intent"
	"github.com/spf13/cobra"
)

func TestIntentList_LegacyLayoutWarnsWithoutMutation(t *testing.T) {
	root := setupIntentReadCommandCampaign(t)
	mustWriteIntentReadCommandIntent(t, filepath.Join(root, "workflow", "intents", "inbox", "20260316-legacy-read.md"), intentcore.StatusInbox, "legacy-read")
	before := intentReadCommandDirTreeSnapshot(t, root)

	cmd := newIntentListReadCommand(t)
	if err := cmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("set format: %v", err)
	}

	stdout, stderr, err := captureIntentReadCommandOutput(t, func() error {
		return runIntentList(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runIntentList() error = %v", err)
	}
	if !strings.Contains(stderr, "camp init --repair") {
		t.Fatalf("stderr = %q, want repair warning", stderr)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("stdout = %q, want empty JSON array", stdout)
	}

	after := intentReadCommandDirTreeSnapshot(t, root)
	if before != after {
		t.Fatalf("intent list mutated filesystem:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestIntentList_ReadOnlyCanonicalDirNoPermissionError(t *testing.T) {
	root := setupIntentReadCommandCampaign(t)
	intentsDir := filepath.Join(root, ".campaign", "intents")
	mustWriteIntentReadCommandIntent(t, filepath.Join(intentsDir, "inbox", "20260316-read-only.md"), intentcore.StatusInbox, "read-only")

	if err := os.Chmod(intentsDir, 0555); err != nil {
		t.Fatalf("chmod read-only: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(intentsDir, 0755)
	})

	cmd := newIntentListReadCommand(t)
	if err := cmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("set format: %v", err)
	}

	_, _, err := captureIntentReadCommandOutput(t, func() error {
		return runIntentList(cmd, nil)
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			t.Fatalf("runIntentList() attempted a write in read-only dir: %v", err)
		}
		t.Fatalf("runIntentList() error = %v", err)
	}
}

func setupIntentReadCommandCampaign(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "settings"), 0755); err != nil {
		t.Fatalf("mkdir campaign settings: %v", err)
	}
	config := "id: test-intent-read\nname: test-intent-read\ntype: product\n"
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"), []byte(config), 0644); err != nil {
		t.Fatalf("write campaign config: %v", err)
	}
	jumps := "paths:\n  intents: .campaign/intents/\n"
	if err := os.WriteFile(filepath.Join(root, ".campaign", "settings", "jumps.yaml"), []byte(jumps), 0644); err != nil {
		t.Fatalf("write jumps config: %v", err)
	}

	t.Setenv(campaign.EnvCampaignRoot, root)
	campaign.ClearCache()
	t.Cleanup(campaign.ClearCache)
	return root
}

func newIntentListReadCommand(t *testing.T) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	flags := cmd.Flags()
	flags.StringP("format", "f", "table", "Output format")
	flags.StringP("sort", "S", "updated", "Sort by")
	flags.StringSliceP("status", "s", nil, "Filter by status")
	flags.StringSliceP("type", "t", nil, "Filter by type")
	flags.StringP("project", "p", "", "Filter by project")
	flags.String("horizon", "", "Filter by horizon")
	flags.IntP("limit", "n", 0, "Limit results")
	flags.BoolP("all", "a", false, "Include dungeon intents")
	return cmd
}

func mustWriteIntentReadCommandIntent(t *testing.T, path string, status intentcore.Status, slug string) {
	t.Helper()

	intent := &intentcore.Intent{
		ID:      "20260316-" + slug,
		Title:   "Intent " + slug,
		Status:  status,
		Type:    intentcore.TypeFeature,
		Content: "test intent\n",
	}
	data, err := intentcore.SerializeIntent(intent)
	if err != nil {
		t.Fatalf("SerializeIntent() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir intent parent: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write intent: %v", err)
	}
}

func captureIntentReadCommandOutput(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW
	runErr := fn()
	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	stdout, readOutErr := io.ReadAll(stdoutR)
	if readOutErr != nil {
		t.Fatalf("read stdout: %v", readOutErr)
	}
	stderr, readErrErr := io.ReadAll(stderrR)
	if readErrErr != nil {
		t.Fatalf("read stderr: %v", readErrErr)
	}
	return string(stdout), string(stderr), runErr
}

func intentReadCommandDirTreeSnapshot(t *testing.T, root string) string {
	t.Helper()

	var entries []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entries = append(entries, fmt.Sprintf("%s|dir=%t|mode=%o|size=%d", rel, d.IsDir(), info.Mode().Perm(), info.Size()))
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n")
}
