package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func TestValidateBackgroundCommit(t *testing.T) {
	cases := []struct {
		name                         string
		background, autoWrite, amend bool
		message                      string
		invalid                      bool
	}{
		{name: "ordinary manual", message: "manual"},
		{name: "ordinary auto-write", autoWrite: true},
		{name: "background", background: true, autoWrite: true},
		{name: "requires auto-write", background: true, invalid: true},
		{name: "rejects amend", background: true, autoWrite: true, amend: true, invalid: true},
		{name: "rejects message", background: true, autoWrite: true, message: "manual", invalid: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBackgroundCommit(tt.background, tt.autoWrite, tt.amend, tt.message)
			if (err != nil) != tt.invalid {
				t.Fatalf("error = %v, want invalid = %v", err, tt.invalid)
			}
		})
	}
}

func TestBackgroundCancellationPrecedesIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := requireBackgroundCommit(ctx, "/not-a-repo", "/not-a-repo"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestBackgroundReceiptWriteFailure(t *testing.T) {
	err := emitBackgroundCommit(failingReceiptWriter{}, newCommitJSONResult("/camp"), backgroundNothingToCommit, nil)
	if !errors.Is(err, errReceiptWrite) {
		t.Fatalf("error = %v, want write failure", err)
	}
}

var errReceiptWrite = errors.New("receipt output closed")

type failingReceiptWriter struct{}

func (failingReceiptWriter) Write([]byte) (int, error) { return 0, errReceiptWrite }

func TestCommitFlagParseErrorUsesBackgroundJSONContract(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		jsonErr bool
	}{
		{
			name:    "background json",
			args:    []string{"commit", "--auto-write", "--background", "--json", "--all=invalid"},
			jsonErr: true,
		},
		{
			name:    "mode flags after the parse failure",
			args:    []string{"commit", "--all=invalid", "--auto-write", "--background", "--json"},
			jsonErr: true,
		},
		{
			name: "legacy commit json",
			args: []string{"commit", "--json", "--all=invalid"},
		},
		{
			name: "explicit json false",
			args: []string{"commit", "--auto-write", "--background", "--json=false", "--all=invalid"},
		},
		{
			name: "explicit background false",
			args: []string{"commit", "--auto-write", "--background=false", "--json", "--all=invalid"},
		},
		{
			name: "background without json",
			args: []string{"commit", "--auto-write", "--background", "--all=invalid"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := executeCommitFlagError(t, tt.args...)
			var cmdErr *camperrors.CommandError
			if !errors.As(err, &cmdErr) {
				t.Fatalf("error = %T %v, want *CommandError", err, err)
			}
			if cmdErr.ExitCode != 2 {
				t.Fatalf("exit code = %d, want 2", cmdErr.ExitCode)
			}
			combined := stdout + stderr
			if tt.jsonErr {
				if strings.Contains(combined, "Usage:") {
					t.Fatalf("background JSON flag error printed usage:\n%s", combined)
				}
				env := decodeBackgroundFlagError(t, stderr)
				if env.SchemaVersion != CommitBackgroundJSONVersion {
					t.Fatalf("schema_version = %q, want %q", env.SchemaVersion, CommitBackgroundJSONVersion)
				}
				if env.Error.Code != "validation_error" || env.Error.ExitCode != 2 || !strings.Contains(env.Error.Message, "invalid") {
					t.Fatalf("error payload = %+v, want validation_error exit 2 mentioning invalid", env.Error)
				}
				return
			}
			if !strings.Contains(stdout, "Usage:") {
				t.Fatalf("legacy flag error output = %q, want usage text", combined)
			}
			if strings.Contains(combined, `"schema_version"`) {
				t.Fatalf("legacy flag error rendered a JSON envelope:\n%s", combined)
			}
		})
	}
}

func executeCommitFlagError(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)

	all, auto, background, jsonOut := commitAll, commitAutoWrite, commitBackground, commitJSONOut
	silenceUsage, silenceErrors := commitCmd.SilenceUsage, commitCmd.SilenceErrors
	rootSilence := rootCmd.SilenceUsage
	oldArgs := os.Args
	os.Args = append([]string{"camp"}, args...)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SilenceUsage = rootSilence
		commitCmd.SilenceUsage = silenceUsage
		commitCmd.SilenceErrors = silenceErrors
		commitAll, commitAutoWrite, commitBackground, commitJSONOut = all, auto, background, jsonOut
		os.Args = oldArgs
	})

	err := Execute(context.Background())
	return stdout.String(), stderr.String(), err
}

func decodeBackgroundFlagError(t *testing.T, raw string) backgroundFlagErrorEnvelope {
	t.Helper()
	var env backgroundFlagErrorEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("stderr is not a JSON envelope: %v\n%s", err, raw)
	}
	return env
}

type backgroundFlagErrorEnvelope struct {
	SchemaVersion string `json:"schema_version"`
	Error         struct {
		Code     string `json:"code"`
		Message  string `json:"message"`
		ExitCode int    `json:"exit_code"`
	} `json:"error"`
}

func TestSynchronousResultHasNoQueueFields(t *testing.T) {
	var out bytes.Buffer
	result := newCommitJSONResult("/camp")
	if err := result.emit(&out); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"job_id"`, `"outcome"`, CommitBackgroundJSONVersion} {
		if bytes.Contains(out.Bytes(), []byte(field)) {
			t.Fatalf("synchronous result contains %s: %s", field, out.String())
		}
	}
}
