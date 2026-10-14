//go:build container_fs

package flow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPowerShellFlowLiteralArgsAndWorkDir(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "Work With Spaces 日本語 [one] O'Brien")
	if err := os.Mkdir(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(root, "flow-result.json")
	f := Flow{
		Command: "& { $result = @{ cwd = $PWD.Path; process_cwd = [IO.Directory]::GetCurrentDirectory(); value = $env:FLOW_VALUE; args = @($args) } | ConvertTo-Json -Compress; [IO.File]::WriteAllText($env:FLOW_OUT, $result) }\n",
		Shell:   powerShellFlowShell(),
		WorkDir: filepath.Base(workDir),
		Env: map[string]string{
			"FLOW_OUT":   outFile,
			"FLOW_VALUE": "value with spaces",
		},
	}
	wantArgs := []string{"two words", "", "O'Brien \"quoted\"", "日本語 argument", "a;b", "$(Write-Output injected)"}
	if err := NewRunner(root).Run(context.Background(), f, wantArgs); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Cwd        string   `json:"cwd"`
		ProcessCwd string   `json:"process_cwd"`
		Value      string   `json:"value"`
		Args       []string `json:"args"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("flow result is not JSON: %v: %s", err, data)
	}
	if !strings.EqualFold(filepath.Clean(got.Cwd), workDir) || !strings.EqualFold(filepath.Clean(got.ProcessCwd), workDir) || got.Value != "value with spaces" || strings.Join(got.Args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("flow result = %+v, want cwd=%q value=%q args=%#v", got, workDir, "value with spaces", wantArgs)
	}
}

func TestPowerShellFlowSyntaxFailureDoesNotRunPrefix(t *testing.T) {
	root := t.TempDir()
	outFile := filepath.Join(root, "unexpected.txt")
	f := Flow{
		Command: `[IO.File]::WriteAllText($env:FLOW_OUT, 'ran'); if (`,
		Shell:   powerShellFlowShell(),
		Env:     map[string]string{"FLOW_OUT": outFile},
	}
	if err := NewRunner(root).Run(context.Background(), f, nil); err == nil {
		t.Fatal("invalid PowerShell flow succeeded")
	}
	if _, err := os.Stat(outFile); !os.IsNotExist(err) {
		t.Fatalf("invalid flow ran its prefix: stat error = %v", err)
	}
}

func TestPowerShellFlowCancellationStopsFollowup(t *testing.T) {
	root := t.TempDir()
	startedFile := filepath.Join(root, "started.txt")
	outFile := filepath.Join(root, "unexpected.txt")
	f := Flow{
		Command: `[IO.File]::WriteAllText($env:FLOW_STARTED, 'started'); Start-Sleep -Seconds 30; [IO.File]::WriteAllText($env:FLOW_OUT, 'ran')`,
		Shell:   powerShellFlowShell(),
		Env:     map[string]string{"FLOW_STARTED": startedFile, "FLOW_OUT": outFile},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- NewRunner(root).Run(ctx, f, nil) }()
	deadline := time.After(10 * time.Second)
	for {
		if _, err := os.Stat(startedFile); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("flow exited before cancellation: %v", err)
		case <-deadline:
			t.Fatal("flow never reached cancellation point")
		case <-time.After(25 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled flow error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled flow did not stop promptly")
	}
	if _, err := os.Stat(outFile); !os.IsNotExist(err) {
		t.Fatalf("cancelled flow ran followup: stat error = %v", err)
	}
}

func powerShellFlowShell() string {
	if runtime.GOOS == "windows" {
		return "" // Prove that Windows defaults to PowerShell.
	}
	return "powershell"
}
