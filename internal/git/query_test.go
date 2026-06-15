package git

import (
	"context"
	"strings"
	"testing"
)

func TestOutputIncludesStderrOnError(t *testing.T) {
	_, err := Output(context.Background(), t.TempDir(), "rev-parse", "--git-dir")
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("expected git stderr in error, got %q", err.Error())
	}
}
