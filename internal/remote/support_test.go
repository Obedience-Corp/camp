package remote

import (
	"errors"
	"strings"
	"testing"
)

func TestPeerSupportContract(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		if err := requirePeerSupportFor(goos); err != nil {
			t.Errorf("%s peer support: %v", goos, err)
		}
	}
	err := requirePeerSupportFor("windows")
	if !errors.Is(err, ErrWindowsPeerUnsupported) {
		t.Fatalf("Windows peer support error = %v", err)
	}
	for _, detail := range []string{"Windows", "local Camp commands", "macOS/Linux"} {
		if !strings.Contains(err.Error(), detail) {
			t.Errorf("Windows peer error omits %q: %v", detail, err)
		}
	}
}
