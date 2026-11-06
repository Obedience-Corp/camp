package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/clone"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/sync"
)

func captureReportingOutput(t *testing.T, run func()) string {
	t.Helper()
	stdout, stderr := os.Stdout, os.Stderr
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outWrite, errWrite
	defer func() { os.Stdout, os.Stderr = stdout, stderr }()
	run()
	if err := outWrite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := errWrite.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(outRead)
	if err != nil {
		t.Fatal(err)
	}
	errOut, err := io.ReadAll(errRead)
	if err != nil {
		t.Fatal(err)
	}
	if err := outRead.Close(); err != nil {
		t.Fatal(err)
	}
	if err := errRead.Close(); err != nil {
		t.Fatal(err)
	}
	return string(out) + string(errOut)
}

func TestFormatCloneHumanShowsFailedModuleWithoutVerbose(t *testing.T) {
	original := errors.New("fatal: repository unavailable")
	result := &clone.CloneResult{Success: false, Directory: "/tmp/checkout", Errors: []error{original},
		Submodules: []clone.SubmoduleResult{{Path: "projects/broken", Operation: "init", Error: original}},
	}
	got := captureReportingOutput(t, func() { formatCloneHuman(result, false) })
	for _, want := range []string{"projects/broken", "(init)", "fatal: repository unavailable", "1 failed"} {
		if !strings.Contains(got, want) {
			t.Errorf("human clone output missing %q: %s", want, got)
		}
	}
	if count := strings.Count(got, "fatal: repository unavailable"); count != 1 {
		t.Fatalf("Git cause repeated %d times: %s", count, got)
	}
}

func TestFormatSyncJSONIncludesOrderedModuleErrors(t *testing.T) {
	result := &sync.SyncResult{Success: false, ValidationRan: true, ValidationFailed: true, UpdateResults: []sync.SubmoduleResult{
		{Path: "zeta", Name: "zeta", Success: true, Initialized: true},
		{Path: "alpha", Name: "alpha", Error: &sync.SyncError{Op: "children", Submodule: "alpha", Cause: errors.New("nested unavailable")},
			Initialized: true, Nested: []git.NestedResult{{Owner: "alpha", Path: "alpha/child", Operation: "init", Attempted: true,
				Error: errors.New("original Git failure")}}},
	}, Errors: []error{errors.New("nested unavailable")}}
	encoded := captureReportingOutput(t, func() { formatSyncJSON(result, nil, 0) })
	var output struct {
		UpdateResults struct {
			Initialized int `json:"initialized"`
			Failed      int `json:"failed"`
			Results     []struct {
				Path      string `json:"path"`
				Operation string `json:"operation"`
				Error     string `json:"error"`
				Nested    []struct {
					Path  string `json:"path"`
					Error string `json:"error"`
				} `json:"nested"`
			} `json:"results"`
		} `json:"updateResults"`
		Errors     []string `json:"errors"`
		Validation *struct {
			Passed bool `json:"passed"`
		} `json:"validation"`
	}
	if err := json.Unmarshal([]byte(encoded), &output); err != nil {
		t.Fatalf("sync JSON: %v\n%s", err, encoded)
	}
	if output.UpdateResults.Initialized != 2 || output.UpdateResults.Failed != 1 || len(output.UpdateResults.Results) != 2 {
		t.Fatalf("sync counts/results = %+v", output.UpdateResults)
	}
	first := output.UpdateResults.Results[0]
	if first.Path != "alpha" || first.Operation != "children" || !strings.Contains(first.Error, "nested unavailable") ||
		len(first.Nested) != 1 || first.Nested[0].Path != "alpha/child" || first.Nested[0].Error != "original Git failure" {
		t.Fatalf("first structured failure = %+v", first)
	}
	if output.UpdateResults.Results[1].Path != "zeta" || len(output.Errors) != 1 || output.Validation == nil || output.Validation.Passed {
		t.Fatalf("ordering/aggregate errors = %+v", output)
	}
}

func TestFormatSyncHumanShowsValidationErrors(t *testing.T) {
	result := &sync.SyncResult{Success: false, PreflightPassed: true, ValidationRan: true, ValidationFailed: true,
		Errors: []error{errors.New("validate-init projects/child: not initialized")}}
	got := captureReportingOutput(t, func() { formatSyncHuman(result, syncOptions{}, &sync.PreflightResult{Passed: true}) })
	if !strings.Contains(got, "validate-init projects/child: not initialized") {
		t.Fatalf("human sync output hid validation error: %s", got)
	}
}

func TestReportingExitCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"clone root failure", determineCloneExitCode(&clone.CloneResult{Success: false}), clone.ExitCloneFailed},
		{"clone total module failure", determineCloneExitCode(&clone.CloneResult{Directory: "/tmp/usable", Submodules: []clone.SubmoduleResult{{Path: "bad"}}}), clone.ExitPartialSuccess},
		{"clone partial module failure", determineCloneExitCode(&clone.CloneResult{Directory: "/tmp/usable", Submodules: []clone.SubmoduleResult{{Path: "good", Success: true}, {Path: "bad"}}}), clone.ExitPartialSuccess},
		{"clone validation only", determineCloneExitCode(&clone.CloneResult{Directory: "/tmp/usable", Validation: &clone.ValidationResult{Passed: false}}), clone.ExitValidationFailed},
		{"clone usable root metadata failure", determineCloneExitCode(&clone.CloneResult{Directory: "/tmp/usable", Errors: []error{errors.New("malformed metadata")}}), clone.ExitPartialSuccess},
		{"sync operation and validation", determineSyncExitCode(&sync.SyncResult{Success: false, PreflightPassed: true, OperationFailed: true, ValidationFailed: true}, syncFlags{}), sync.ExitSyncFailed},
		{"sync validation only", determineSyncExitCode(&sync.SyncResult{Success: false, PreflightPassed: true, ValidationFailed: true}, syncFlags{}), sync.ExitValidationFailed},
		{"sync preflight", determineSyncExitCode(&sync.SyncResult{Success: false}, syncFlags{}), sync.ExitPreflightFailed},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var commandErr *camperrors.CommandError
			if !errors.As(test.err, &commandErr) || commandErr.ExitCode != test.want {
				t.Fatalf("exit = %v, want %d", test.err, test.want)
			}
		})
	}
	if err := determineSyncExitCode(&sync.SyncResult{Success: true}, syncFlags{}); err != nil {
		t.Fatalf("successful optional-only sync: %v", err)
	}
}
