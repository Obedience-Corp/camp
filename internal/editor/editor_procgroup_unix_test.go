//go:build unix

package editor

import (
	"context"
	"testing"
)

func TestBuildEditorCommand_DoesNotSetProcessGroup(t *testing.T) {
	cmd := BuildEditorCommand(context.Background(), "nvim", "/tmp/test.md")
	if cmd == nil {
		t.Fatal("BuildEditorCommand returned nil")
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid {
		t.Fatal("BuildEditorCommand should not force a separate process group")
	}
}
