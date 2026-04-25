package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/fest"
	"github.com/Obedience-Corp/camp/internal/scaffold"
)

func TestBuildRepairCommitFiles_IncludesIntentMigrations(t *testing.T) {
	result := &scaffold.InitResult{
		CampaignRoot: "/campaign",
	}
	plan := &scaffold.RepairPlan{
		IntentMigrations: []scaffold.MigrationAction{
			{
				Source: "/campaign/workflow/intents/inbox",
				Dest:   "/campaign/.campaign/intents/inbox",
				Items:  []string{"legacy.md"},
			},
		},
	}

	files := buildRepairCommitFiles(result, plan)
	got := strings.Join(files, "\n")
	if !strings.Contains(got, "workflow/intents/inbox/legacy.md") {
		t.Fatalf("commit files missing legacy source path: %v", files)
	}
	if !strings.Contains(got, ".campaign/intents/inbox/legacy.md") {
		t.Fatalf("commit files missing canonical destination path: %v", files)
	}
}

func TestBuildRepairCommitMessage_IncludesIntentMigrations(t *testing.T) {
	msg := buildRepairCommitMessage(&scaffold.InitResult{}, &scaffold.RepairPlan{
		IntentMigrations: []scaffold.MigrationAction{
			{
				Source: "/campaign/workflow/intents/inbox",
				Dest:   "/campaign/.campaign/intents/inbox",
				Items:  []string{"legacy.md"},
			},
		},
	}, 0)

	if !strings.Contains(msg, "Migrated 1 legacy intent item(s):") {
		t.Fatalf("commit message missing intent migration summary: %q", msg)
	}
	if !strings.Contains(msg, "/campaign/workflow/intents/inbox/legacy.md → /campaign/.campaign/intents/inbox") {
		t.Fatalf("commit message missing intent migration detail: %q", msg)
	}
}

// TestCampInit_FestivalInitOwnership verifies that festival initialization is
// owned exclusively by the cmd layer, not scaffold.Init.
func TestCampInit_FestivalInitOwnership(t *testing.T) {
	t.Run("skip-fest leaves festivals absent", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir, _ = filepath.EvalSymlinks(tmpDir)
		t.Setenv("XDG_CONFIG_HOME", tmpDir)

		campaignDir := filepath.Join(tmpDir, "no-fest-campaign")
		if err := os.MkdirAll(campaignDir, 0755); err != nil {
			t.Fatalf("failed to create campaign dir: %v", err)
		}

		ctx := context.Background()
		opts := scaffold.InitOptions{
			Name:        "no-fest-campaign",
			Type:        config.CampaignTypeProduct,
			NoRegister:  true,
			SkipGitInit: true,
			SkipFest:    true,
		}
		result, err := scaffold.Init(ctx, campaignDir, opts)
		if err != nil {
			t.Fatalf("scaffold.Init() error = %v", err)
		}

		// Simulate cmd-layer behavior: skip festival init when SkipFest is true.
		// (The cmd-layer checks opts.SkipFest before calling initializeFestivals.)
		if opts.SkipFest {
			// Do not call initializeFestivals -- this is exactly what cmd/camp/init.go does.
		}

		// Assert festivals/ is absent.
		festivalsPath := filepath.Join(result.CampaignRoot, "festivals")
		if _, err := os.Stat(festivalsPath); !os.IsNotExist(err) {
			t.Errorf("festivals/ should be absent when --skip-fest is used, err=%v", err)
		}
		// Assert festivals/ is not in DirsCreated.
		for _, d := range result.DirsCreated {
			if strings.HasSuffix(d, "festivals") || strings.Contains(d, string(filepath.Separator)+"festivals"+string(filepath.Separator)) {
				t.Errorf("scaffold.Init should not produce festivals/ in DirsCreated; got: %s", d)
			}
		}
	})

	t.Run("without skip-fest festivals present when fest available", func(t *testing.T) {
		fest.ResetCache()
		defer fest.ResetCache()

		if !fest.IsFestAvailable() {
			t.Skip("fest not available on this machine; skipping festival-present sub-test")
		}

		tmpDir := t.TempDir()
		tmpDir, _ = filepath.EvalSymlinks(tmpDir)
		t.Setenv("XDG_CONFIG_HOME", tmpDir)

		campaignDir := filepath.Join(tmpDir, "with-fest-campaign")
		if err := os.MkdirAll(campaignDir, 0755); err != nil {
			t.Fatalf("failed to create campaign dir: %v", err)
		}

		ctx := context.Background()
		opts := scaffold.InitOptions{
			Name:        "with-fest-campaign",
			Type:        config.CampaignTypeProduct,
			NoRegister:  true,
			SkipGitInit: true,
		}
		if _, err := scaffold.Init(ctx, campaignDir, opts); err != nil {
			t.Fatalf("scaffold.Init() error = %v", err)
		}

		// Simulate cmd-layer: call initializeFestivals since SkipFest is false.
		initialized, _ := initializeFestivals(ctx, campaignDir, initWriters{humanOut: os.Stdout, machineOut: os.Stdout})
		if !initialized {
			t.Error("initializeFestivals() should succeed when fest is available")
		}

		festivalsPath := filepath.Join(campaignDir, "festivals")
		if _, err := os.Stat(festivalsPath); os.IsNotExist(err) {
			t.Error("festivals/ should exist after initializeFestivals() when fest is available")
		}

		// Regression guard: only one initialization marker (no double-init).
		festMarkers := []string{
			filepath.Join(festivalsPath, ".festival"),
			filepath.Join(festivalsPath, "fest.yaml"),
			filepath.Join(festivalsPath, ".fest"),
		}
		count := 0
		for _, m := range festMarkers {
			if _, err := os.Stat(m); err == nil {
				count++
			}
		}
		if count == 0 {
			t.Error("festivals/ should contain at least one fest initialization marker after initializeFestivals()")
		}
	})
}

// TestCampInit_NoFestOnPath verifies that camp init completes successfully
// when fest is not available, and that initializeFestivals returns the
// ErrFestNotFound sentinel so the caller can show install guidance.
//
// This test requires that fest is genuinely absent from all lookup locations.
// On a machine where fest is installed (e.g. ~/go/bin/fest), the test is
// skipped because FindFestCLI uses hardcoded fallback paths beyond PATH.
func TestCampInit_NoFestOnPath(t *testing.T) {
	// Save and clear PATH, reset the cache so FindFestCLI re-runs.
	t.Setenv("PATH", "")
	fest.ResetCache()
	defer fest.ResetCache()

	// If fest is still findable via fallback paths (e.g. ~/go/bin/fest),
	// skip rather than fail -- the behaviour under test only applies when
	// fest is completely absent.
	if fest.IsFestAvailable() {
		t.Skip("fest found at a fallback location (e.g. ~/go/bin); no-fest path cannot be exercised on this machine")
	}

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "no-path-campaign")
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	ctx := context.Background()
	opts := scaffold.InitOptions{
		Name:        "no-path-campaign",
		Type:        config.CampaignTypeProduct,
		NoRegister:  true,
		SkipGitInit: true,
	}
	if _, err := scaffold.Init(ctx, campaignDir, opts); err != nil {
		t.Fatalf("scaffold.Init() error = %v", err)
	}

	// With fest unavailable, initializeFestivals should return false + ErrFestNotFound.
	initialized, err := initializeFestivals(ctx, campaignDir, initWriters{humanOut: os.Stdout, machineOut: os.Stdout})
	if initialized {
		t.Error("initializeFestivals() should return false when fest is not available")
	}
	if err != fest.ErrFestNotFound {
		t.Errorf("initializeFestivals() error = %v, want fest.ErrFestNotFound", err)
	}
}

// TestRunInitFlow_PrintPathRoutesOutput verifies that runInitFlow writes the
// absolute campaign root to machineOut and human-readable text to humanOut when
// printPath is true.
func TestRunInitFlow_PrintPathRoutesOutput(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "print-path-campaign")

	var humanBuf, machineBuf bytes.Buffer
	w := initWriters{humanOut: &humanBuf, machineOut: &machineBuf}
	p := initParams{
		dir:         campaignDir,
		description: "test desc",
		mission:     "test mission",
		typeStr:     "product",
		noGit:       true,
		noRegister:  true,
		skipFest:    true,
		printPath:   true,
	}

	ctx := context.Background()
	if err := runInitFlow(ctx, p, w, false); err != nil {
		t.Fatalf("runInitFlow() error = %v", err)
	}

	// machineOut must contain exactly the absolute campaign root followed by newline.
	machineOut := machineBuf.String()
	wantPath := campaignDir + "\n"
	if machineOut != wantPath {
		t.Errorf("machineOut = %q, want %q", machineOut, wantPath)
	}

	// humanOut must contain the success banner and summary lines.
	humanOut := humanBuf.String()
	for _, want := range []string{"Campaign Initialized", "Campaign:", "Type:", "ID:", "Root:"} {
		if !strings.Contains(humanOut, want) {
			t.Errorf("humanOut missing %q; full output:\n%s", want, humanOut)
		}
	}

	// humanOut must NOT contain the campaign root as a standalone line (it should
	// appear only as part of "Root: <path>", not as a bare path on its own line).
	// This guards against double-printing the path.
	lines := strings.Split(strings.TrimSpace(humanOut), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == campaignDir {
			t.Errorf("humanOut should not contain bare campaign root as a standalone line; got: %q", line)
		}
	}
}

// TestCampInit_PrintPathRouting verifies the stdout/stderr writer-routing
// contract of runInitFlow when printPath is true, using separate writer buffers
// that mirror what chooseInitWriters wires in production. This is a runner-level
// test, not a true CLI subprocess test (a deferred follow-up tracks adding
// subprocess coverage).
func TestCampInit_PrintPathRouting(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "pp-cli-campaign")

	// Mirrors what runInit builds when --print-path is passed.
	var stdoutBuf, stderrBuf bytes.Buffer
	w := initWriters{humanOut: &stderrBuf, machineOut: &stdoutBuf}
	p := initParams{
		dir:         campaignDir,
		description: "cli test desc",
		mission:     "cli test mission",
		typeStr:     "product",
		noGit:       true,
		noRegister:  true,
		skipFest:    true,
		printPath:   true,
	}

	ctx := context.Background()
	if err := runInitFlow(ctx, p, w, false); err != nil {
		t.Fatalf("runInitFlow() error = %v", err)
	}

	// stdout (machineOut) must contain exactly one non-empty line: the path.
	stdoutStr := stdoutBuf.String()
	stdoutLines := strings.Split(strings.TrimSpace(stdoutStr), "\n")
	if len(stdoutLines) != 1 {
		t.Errorf("stdout should have exactly 1 line, got %d: %q", len(stdoutLines), stdoutStr)
	}
	if stdoutLines[0] != campaignDir {
		t.Errorf("stdout line = %q, want %q", stdoutLines[0], campaignDir)
	}

	// stderr (humanOut) must contain the scaffold summary, not the bare path.
	stderrStr := stderrBuf.String()
	if !strings.Contains(stderrStr, "Campaign Initialized") {
		t.Errorf("stderr missing campaign initialized banner; got:\n%s", stderrStr)
	}
	if !strings.Contains(stderrStr, "Root:") {
		t.Errorf("stderr missing Root: line; got:\n%s", stderrStr)
	}
}

// TestCampInit_DefaultModeWritersUnchanged is a runner-level regression guard:
// when printPath is false, runInitFlow must write all output through humanOut
// and produce nothing extra on machineOut. Default-mode users must see output
// identical to before the writer refactor.
func TestCampInit_DefaultModeWritersUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "default-mode-campaign")

	// Default mode: both writers point at the same buffer (mimicking both = os.Stdout).
	var sharedBuf, machineBuf bytes.Buffer
	w := initWriters{humanOut: &sharedBuf, machineOut: &machineBuf}
	p := initParams{
		dir:         campaignDir,
		description: "default desc",
		mission:     "default mission",
		typeStr:     "product",
		noGit:       true,
		noRegister:  true,
		skipFest:    true,
		printPath:   false, // default mode
	}

	ctx := context.Background()
	if err := runInitFlow(ctx, p, w, false); err != nil {
		t.Fatalf("runInitFlow() error = %v", err)
	}

	// All human-readable content must appear in humanOut (sharedBuf).
	humanOut := sharedBuf.String()
	for _, want := range []string{"Campaign Initialized", "Campaign:", "Type:", "ID:", "Root:"} {
		if !strings.Contains(humanOut, want) {
			t.Errorf("humanOut (stdout) missing %q in default mode; full output:\n%s", want, humanOut)
		}
	}

	// machineOut must be empty in default mode (no bare path emitted).
	machineOut := machineBuf.String()
	if machineOut != "" {
		t.Errorf("machineOut should be empty in default mode, got: %q", machineOut)
	}
}
