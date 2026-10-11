package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestValidateBackgroundCommit(t *testing.T) {
	cases := []struct {
		name                         string
		background, autoWrite, amend bool
		message                      string
		invalid                      bool
	}{
		{name: "ordinary manual", message: "manual"},
		{name: "ordinary auto-write", autoWrite: true},
		{name: "background", background: true, autoWrite: true},
		{name: "requires auto-write", background: true, invalid: true},
		{name: "rejects amend", background: true, autoWrite: true, amend: true, invalid: true},
		{name: "rejects message", background: true, autoWrite: true, message: "manual", invalid: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBackgroundCommit(tt.background, tt.autoWrite, tt.amend, tt.message)
			if (err != nil) != tt.invalid {
				t.Fatalf("error = %v, want invalid = %v", err, tt.invalid)
			}
		})
	}
}

func TestBackgroundCancellationPrecedesIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := requireBackgroundCommit(ctx, "/not-a-repo", "/not-a-repo"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestBackgroundReceiptWriteFailure(t *testing.T) {
	err := emitBackgroundCommit(failingReceiptWriter{}, newCommitJSONResult("/camp"), backgroundNothingToCommit, nil)
	if !errors.Is(err, errReceiptWrite) {
		t.Fatalf("error = %v, want write failure", err)
	}
}

var errReceiptWrite = errors.New("receipt output closed")

type failingReceiptWriter struct{}

func (failingReceiptWriter) Write([]byte) (int, error) { return 0, errReceiptWrite }

func TestSynchronousResultHasNoQueueFields(t *testing.T) {
	var out bytes.Buffer
	result := newCommitJSONResult("/camp")
	if err := result.emit(&out); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"job_id"`, `"outcome"`, CommitBackgroundJSONVersion} {
		if bytes.Contains(out.Bytes(), []byte(field)) {
			t.Fatalf("synchronous result contains %s: %s", field, out.String())
		}
	}
}
