//go:build !failpoint_enabled
// +build !failpoint_enabled

package failpoint

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestTrigger_PanicAction_SuppressedInProductionBuild is the regression for
// the obey-agent PR #312 review concern: a leaked CAMP_TEST_FAILPOINT=site=panic
// must NOT panic in a production binary. The default build (no
// `failpoint_enabled` tag) routes panic actions through actions_safe.go,
// which returns a failpointError instead. This test compiles into the same
// default build path as a shipped binary, so if the build-tag boundary is
// ever weakened the test fails.
func TestTrigger_PanicAction_SuppressedInProductionBuild(t *testing.T) {
	t.Setenv(envName, SiteAtomicWriteAfterFsync+"=panic")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("production build must not panic on ActionPanic; recovered: %v", r)
		}
	}()
	err := Trigger(context.Background(), SiteAtomicWriteAfterFsync)
	if err == nil {
		t.Fatal("expected suppressed-panic error, got nil")
	}
	var fp failpointError
	if !errors.As(err, &fp) {
		t.Fatalf("err = %T %v, want failpointError", err, err)
	}
	if !strings.Contains(fp.site, "panic suppressed") {
		t.Errorf("error must explain the suppression so leaked env var is debuggable, got: %q", fp.site)
	}
}

// TestTrigger_KillAction_SuppressedInProductionBuild is the corresponding
// regression for ActionKill: an env-driven kill must NOT terminate a
// production binary. A test that os.Exit'd would take the whole test
// process down, so this assertion is exactly what we want: the call
// returns instead of exiting.
func TestTrigger_KillAction_SuppressedInProductionBuild(t *testing.T) {
	t.Setenv(envName, SiteAtomicWriteAfterFsync+"=kill")
	err := Trigger(context.Background(), SiteAtomicWriteAfterFsync)
	if err == nil {
		t.Fatal("expected suppressed-kill error, got nil")
	}
	var fp failpointError
	if !errors.As(err, &fp) {
		t.Fatalf("err = %T %v, want failpointError", err, err)
	}
	if !strings.Contains(fp.site, "kill suppressed") {
		t.Errorf("error must explain the suppression so leaked env var is debuggable, got: %q", fp.site)
	}
}
