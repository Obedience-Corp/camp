package attach

import (
	"io"
	"testing"
)

func TestDetachCommand_HiddenAndDeprecated(t *testing.T) {
	cmd := NewDetachCommand()
	if !cmd.Hidden {
		t.Error("detach command Hidden = false, want true")
	}
	if cmd.Deprecated == "" {
		t.Error("detach command Deprecated is empty, want a deprecation notice")
	}
}

func TestAttachCommand_StaysVisible(t *testing.T) {
	cmd := NewAttachCommand(func(_ io.Writer, _ string) CampaignResolver {
		return nil
	})
	if cmd.Hidden {
		t.Error("attach command Hidden = true, want false: camp attach is unaffected by the camp detach deprecation")
	}
	if cmd.Deprecated != "" {
		t.Errorf("attach command Deprecated = %q, want empty", cmd.Deprecated)
	}
}
