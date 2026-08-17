// Package itestenv owns the environment the integration suite runs against:
// which Docker daemon it uses, who is allowed to use it right now, and whether
// that daemon is healthy enough to start a run at all.
//
// It exists because the suite's capacity model assumed an idle, exclusively
// owned daemon while the daemon it actually used was Colima's default profile:
// the machine's general-purpose one, with long-lived residents on it and any
// concurrent agent session's gates landing beside the pool. That assumption
// failing is what turned occasional flakes into routine mid-run collapse, most
// visibly on 2026-08-10 (6 failed / 38 passed / 871 skipped on a commit where
// all 915 tests pass).
//
// Three pieces answer that, in the order a run needs them:
//
//  1. Resolve picks a dedicated Colima profile (camp-itest) and starts it on
//     demand, so co-tenant load cannot reach the suite. When there is no Colima
//     to isolate with, it falls back to the shared daemon and says so loudly
//     rather than pretending.
//  2. Acquire takes a machine-wide lock keyed by the resolved daemon, so two
//     suites pointed at the same VM serialize instead of collapsing each other.
//  3. Probe measures a daemon round trip before the pool is built, so a daemon
//     that is already wedged costs five seconds instead of a wedged run.
//
// The package is shared by the dashboard runner (internal/buildutil/tasks) and
// by the suite's own TestMain, so both lanes resolve, lock, and probe the same
// way. Nothing here is product code: it configures how camp is tested.
package itestenv
