package jobs

import (
	"errors"
	"strings"
	"testing"
)

func TestHeadMovedMessageUnborn(t *testing.T) {
	early := headMovedMessage("", "")
	if !strings.Contains(early, "HEAD is no longer unborn") {
		t.Fatalf("early unborn-moved message = %q, want the unborn contract", early)
	}
	late := headMovedMessage("", "abcdef1234567890")
	if !strings.Contains(late, "HEAD is no longer unborn") {
		t.Fatalf("late unborn-moved message = %q, want the unborn contract", late)
	}
	if strings.Contains(early, "expected parent") || strings.Contains(late, "expected parent") {
		t.Fatalf("unborn-moved messages must not invent an expected parent: %q / %q", early, late)
	}
}

func TestHeadMovedMessageBorn(t *testing.T) {
	parent := "aaaaaaaaaaaaaaaa"
	msg := headMovedMessage(parent, "")
	if !strings.Contains(msg, "HEAD moved since this commit was queued") {
		t.Fatalf("born-moved message = %q", msg)
	}
	if !strings.Contains(msg, shortSHA(parent)) {
		t.Fatalf("born-moved message missing parent: %q", msg)
	}
	late := headMovedMessage(parent, "bbbbbbbbbbbbbbbb")
	if !strings.Contains(late, shortSHA("bbbbbbbbbbbbbbbb")) {
		t.Fatalf("late born-moved message missing new SHA: %q", late)
	}
}

func TestHeadMovedErrorWrapsCause(t *testing.T) {
	cause := errors.New("move HEAD to the deferred commit")
	err := headMovedError(cause, "", "deadbeefdeadbeef")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "HEAD is no longer unborn") {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("cause not wrapped: %v", err)
	}
}
