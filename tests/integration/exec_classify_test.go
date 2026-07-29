//go:build integration
// +build integration

package integration

import (
	"errors"
	"strings"
	"testing"
)

// A Docker transport fault must never read as a failure of whatever test
// happened to be running when the daemon dropped the stream.
func TestClassifyExecError(t *testing.T) {
	t.Parallel()

	transport := []string{
		"container exec attach: unexpected EOF",
		"read tcp 127.0.0.1:1234: connection reset by peer",
		"write unix /var/run/docker.sock: broken pipe",
		"Error response from daemon: Container abc123 is not running",
		"Cannot connect to the Docker daemon at unix:///var/run/docker.sock",
	}
	for _, msg := range transport {
		got := classifyExecError(errors.New(msg))
		if !strings.Contains(got.Error(), "INFRASTRUCTURE FAILURE (not a test failure)") {
			t.Errorf("classifyExecError(%q) was not labelled as infrastructure:\n%s", msg, got)
		}
		if !errors.Is(got, errors.Unwrap(got)) {
			t.Errorf("classifyExecError(%q) must wrap the original so the cause survives", msg)
		}
	}

	// A command that ran and reported something is not an infrastructure
	// fault. Labelling those would hide real failures behind a re-run
	// suggestion, which is worse than the noise it removes.
	real := []string{
		"exit status 1",
		"fatal: not a git repository",
		"camp: unknown command",
	}
	for _, msg := range real {
		err := errors.New(msg)
		if got := classifyExecError(err); got != err {
			t.Errorf("classifyExecError(%q) relabelled a real failure as infrastructure:\n%s", msg, got)
		}
	}

	if classifyExecError(nil) != nil {
		t.Error("classifyExecError(nil) must stay nil")
	}
}
