// Package failpoint exposes hooks that integration tests can use to inject
// failures at named call sites. In normal builds every hook is a no-op so
// production paths pay zero overhead; tests set CAMP_TEST_FAILPOINT to
// trigger a panic, kill, or error at the named site.
//
// Safety: destructive actions (panic / kill) are GATED BEHIND THE
// `failpoint_enabled` BUILD TAG. The default production build compiles
// `actions_safe.go`, which converts panic/kill into ActionError responses
// so a leaked CAMP_TEST_FAILPOINT cannot terminate a real `camp`. Test
// runs that need destructive injection must build with
// `-tags failpoint_enabled`, which swaps in `actions_unsafe.go`. The
// build-tag boundary makes it impossible for an env-var alone to take
// down a production binary.
//
// This is the scaffold the CW0003 production-readiness audit asked for: the
// 14 "needs failpoint harness" cells in PRODUCTION_READINESS.md (kill-mid-
// rename, kill-mid-commit, ENOSPC etc.) all depend on this mechanism. The
// kill-injection integration tests themselves land as follow-up work in a
// future sequence; this scaffold establishes the API + the no-op path so
// production code can be instrumented without waiting.
package failpoint

import (
	"context"
	"os"
	"strings"
)

// Sentinel site names. Production code calls Trigger(ctx, <name>) at known
// hot spots; failpoint-enabled test runs match the name against the
// CAMP_TEST_FAILPOINT env var.
const (
	SiteAtomicWriteAfterFsync     = "fsutil.atomic_write.after_fsync"
	SiteCommitAfterStageBeforeGit = "git.commit.after_stage_before_commit"
	SiteBackfillRefMidQueue       = "workitem.backfill_ref.mid_queue"
)

// Action enumerates what a triggered failpoint does.
type Action string

const (
	ActionNone  Action = ""
	ActionPanic Action = "panic"
	ActionKill  Action = "kill"
	ActionError Action = "error"
)

// Trigger checks whether the named failpoint is enabled and, if so, performs
// the configured Action. Returns a non-nil error only when Action=error.
// In normal builds (no CAMP_TEST_FAILPOINT set) this is a single env-var
// read and a string compare; the no-op path stays under ~50ns.
//
// In default production builds (without the `failpoint_enabled` build tag),
// ActionPanic and ActionKill are downgraded to ActionError so a leaked
// CAMP_TEST_FAILPOINT cannot terminate the process. See actions_safe.go.
func Trigger(ctx context.Context, site string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !enabled() {
		return nil
	}
	action := lookup(site)
	switch action {
	case ActionNone:
		return nil
	case ActionPanic:
		return runPanic(site)
	case ActionKill:
		return runKill(site)
	case ActionError:
		return failpointError{site: site}
	}
	return nil
}

// Enabled reports whether the failpoint mechanism is active. Cheap predicate
// for production code that wants to skip optional hooks when not under test.
func Enabled() bool { return enabled() }

type failpointError struct{ site string }

func (e failpointError) Error() string { return "failpoint triggered: " + e.site }

func enabled() bool {
	return os.Getenv(envName) != ""
}

func lookup(site string) Action {
	spec := os.Getenv(envName)
	if spec == "" {
		return ActionNone
	}
	for _, entry := range strings.Split(spec, ",") {
		name, action, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok {
			continue
		}
		if name == site {
			return Action(action)
		}
	}
	return ActionNone
}

const envName = "CAMP_TEST_FAILPOINT"
