package failpoint

import (
	"context"
	"errors"
	"testing"
)

func TestTrigger_NoEnv_IsNoOp(t *testing.T) {
	t.Setenv(envName, "")
	if err := Trigger(context.Background(), SiteAtomicWriteAfterFsync); err != nil {
		t.Fatalf("expected no-op, got %v", err)
	}
	if Enabled() {
		t.Fatal("Enabled() should be false when env unset")
	}
}

func TestTrigger_UnmatchedSite_IsNoOp(t *testing.T) {
	t.Setenv(envName, SiteBackfillRefMidQueue+"=error")
	if err := Trigger(context.Background(), SiteAtomicWriteAfterFsync); err != nil {
		t.Fatalf("unmatched site should be no-op, got %v", err)
	}
}

func TestTrigger_ErrorAction_ReturnsError(t *testing.T) {
	t.Setenv(envName, SiteAtomicWriteAfterFsync+"=error")
	err := Trigger(context.Background(), SiteAtomicWriteAfterFsync)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var fp failpointError
	if !errors.As(err, &fp) || fp.site != SiteAtomicWriteAfterFsync {
		t.Fatalf("err = %v, want failpointError for the matched site", err)
	}
}

func TestTrigger_CanceledCtx_TakesPrecedence(t *testing.T) {
	t.Setenv(envName, SiteAtomicWriteAfterFsync+"=error")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Trigger(ctx, SiteAtomicWriteAfterFsync)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
