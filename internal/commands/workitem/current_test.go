package workitem

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCurrentTombstone_RejectsLegacyArgvWithJSONEnvelope(t *testing.T) {
	root := NewWorkitemCommand()
	// Mimic the exact legacy argv that festival-app still issues.
	root.SetArgs([]string{"current", "--json"})
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	err := root.Execute()
	if err == nil {
		t.Fatal("expected tombstone to fail; got success (would mean list fallthrough)")
	}

	if stdout.Len() != 0 {
		t.Fatalf("stdout must be empty for --json refusal, got %q", stdout.String())
	}
	if strings.Contains(stderr.String(), "workitems/v1alpha10") {
		t.Fatalf("must not emit parent list schema: %s", stderr.String())
	}

	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		Error         struct {
			Code     string `json:"code"`
			Message  string `json:"message"`
			Hint     string `json:"hint"`
			ExitCode int    `json:"exit_code"`
		} `json:"error"`
	}
	if uerr := json.Unmarshal(stderr.Bytes(), &envelope); uerr != nil {
		t.Fatalf("stderr is not a JSON envelope: %v\n%s", uerr, stderr.String())
	}
	if envelope.SchemaVersion != CurrentRemovedSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", envelope.SchemaVersion, CurrentRemovedSchemaVersion)
	}
	if envelope.Error.Code != "validation_error" {
		t.Fatalf("error.code = %q, want validation_error", envelope.Error.Code)
	}
	if envelope.Error.Message != currentRemovedMessage {
		t.Fatalf("error.message = %q, want %q", envelope.Error.Message, currentRemovedMessage)
	}
	if !strings.Contains(envelope.Error.Hint, "camp workitem link") {
		t.Fatalf("hint missing migration path: %q", envelope.Error.Hint)
	}
	if envelope.Error.ExitCode != 2 {
		t.Fatalf("exit_code = %d, want 2", envelope.Error.ExitCode)
	}
}

func TestCurrentTombstone_RejectsClearAndSelector(t *testing.T) {
	for _, args := range [][]string{
		{"current"},
		{"current", "--clear"},
		{"current", "some-workitem"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := NewWorkitemCommand()
			root.SetArgs(args)
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			if err := root.Execute(); err == nil {
				t.Fatal("expected removal error")
			} else if !strings.Contains(err.Error(), currentRemovedMessage) {
				t.Fatalf("error = %v, want message containing %q", err, currentRemovedMessage)
			}
		})
	}
}

func TestCurrentTombstone_IsRegisteredNotHidden(t *testing.T) {
	root := NewWorkitemCommand()
	child, _, err := root.Find([]string{"current"})
	if err != nil || child == nil || child.Name() != "current" {
		t.Fatalf("current tombstone not registered: child=%v err=%v", child, err)
	}
	if child.Hidden {
		t.Fatal("tombstone must not be Hidden; legacy argv must resolve to it")
	}
	if !strings.Contains(strings.ToLower(child.Short), "removed") {
		t.Fatalf("Short = %q, want it to say removed", child.Short)
	}
}
