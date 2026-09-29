package main

import (
	"errors"
	"testing"

	"github.com/Obedience-Corp/camp/internal/starter"
)

type starterOutputWriter struct {
	calls  int
	failAt int
	err    error
}

func (w *starterOutputWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failAt {
		return 0, w.err
	}
	return len(p), nil
}

func TestRenderStarterResultPreservesWriteErrors(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		outputErr := errors.New("output unavailable")
		out := &starterOutputWriter{failAt: failAt, err: outputErr}
		err := renderStarterResult(out, starter.Result{
			Action: "registered", Path: "/camps/festival", Message: "Unregistered starter files remain at /camps/staging",
		})
		if !errors.Is(err, outputErr) {
			t.Fatalf("write %d: got %v, want %v", failAt, err, outputErr)
		}
		if out.calls != failAt {
			t.Fatalf("continued writing after failure: got %d writes, want %d", out.calls, failAt)
		}
	}
}
