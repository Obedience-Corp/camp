package ui

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	osc52 "github.com/aymanbagabas/go-osc52/v2"
)

func stubClipboard(t *testing.T) *bytes.Buffer {
	t.Helper()
	origOutput := clipboardOutput
	origRemote := remoteClipboardSession
	origNative := nativeClipboardWrite
	t.Cleanup(func() {
		clipboardOutput = origOutput
		remoteClipboardSession = origRemote
		nativeClipboardWrite = origNative
	})

	output := &bytes.Buffer{}
	clipboardOutput = output
	return output
}

func TestWriteClipboardRemoteUsesTerminalClipboard(t *testing.T) {
	output := stubClipboard(t)
	remoteClipboardSession = func() bool { return true }
	nativeClipboardWrite = func(string) error {
		t.Fatal("remote copy called the host clipboard command")
		return nil
	}

	const text = "https://login.tailscale.com/a/test"
	if err := writeClipboard(text); err != nil {
		t.Fatalf("writeClipboard: %v", err)
	}
	if got, want := output.String(), osc52.New(text).String(); got != want {
		t.Errorf("terminal sequence = %q, want %q", got, want)
	}
}

func TestWriteClipboardLocalPrefersNativeClipboard(t *testing.T) {
	output := stubClipboard(t)
	remoteClipboardSession = func() bool { return false }
	var copied string
	nativeClipboardWrite = func(text string) error {
		copied = text
		return nil
	}

	if err := writeClipboard("approval-url"); err != nil {
		t.Fatalf("writeClipboard: %v", err)
	}
	if copied != "approval-url" {
		t.Errorf("native clipboard received %q", copied)
	}
	if output.Len() != 0 {
		t.Errorf("terminal clipboard used after native success: %q", output.String())
	}
}

func TestWriteClipboardFallsBackToTerminal(t *testing.T) {
	output := stubClipboard(t)
	remoteClipboardSession = func() bool { return false }
	nativeClipboardWrite = func(string) error { return errors.New("no native clipboard") }

	if err := writeClipboard("approval-url"); err != nil {
		t.Fatalf("writeClipboard: %v", err)
	}
	if got, want := output.String(), osc52.New("approval-url").String(); got != want {
		t.Errorf("terminal sequence = %q, want %q", got, want)
	}
}

func TestWriteClipboardReportsNativeAndTerminalFailures(t *testing.T) {
	stubClipboard(t)
	remoteClipboardSession = func() bool { return false }
	nativeClipboardWrite = func(string) error { return errors.New("native failed") }
	clipboardOutput = errorWriter{err: errors.New("terminal failed")}

	err := writeClipboard("approval-url")
	if err == nil {
		t.Fatal("writeClipboard unexpectedly succeeded")
	}
	for _, want := range []string{"native failed", "terminal failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }

var _ io.Writer = errorWriter{}
