package clone

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatSSHError_PublicKey(t *testing.T) {
	result := FormatSSHError(errors.New("Permission denied (publickey)"))
	if !strings.Contains(result, "SSH key not configured") {
		t.Error("FormatSSHError should mention SSH key configuration")
	}
	if !strings.Contains(result, "ssh -T git@github.com") {
		t.Error("FormatSSHError should include verification command")
	}
}

func TestFormatSSHError_HostKey(t *testing.T) {
	result := FormatSSHError(errors.New("Host key verification failed"))
	if !strings.Contains(result, "GitHub host key not verified") {
		t.Error("FormatSSHError should mention host key verification")
	}
	if !strings.Contains(result, "ssh-keyscan github.com") {
		t.Error("FormatSSHError should include fix command")
	}
}

func TestFormatSSHError_Other(t *testing.T) {
	err := errors.New("some other error")
	result := FormatSSHError(err)
	if result != "some other error" {
		t.Errorf("FormatSSHError() = %q, want original error", result)
	}
}
