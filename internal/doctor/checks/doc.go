// Package checks provides health check implementations for campaign diagnostics.
//
// Each check implements the [doctor.Check] interface and focuses on a specific
// aspect of campaign health:
//
//   - URLCheck: Detects URL mismatches between .gitmodules and .git/config
//   - IntegrityCheck: Verifies submodule directories are not empty or broken
//   - HeadCheck: Identifies detached HEAD states with unpushed commits
//   - WorkingCheck: Finds uncommitted changes in submodules
//   - CommitsCheck: Verifies parent-submodule commit synchronization
//
// Checks are designed to be independent, safe to run concurrently, and must
// respect context cancellation.
//
// # Usage
//
// Checks are registered with a [doctor.Doctor] instance:
//
//	d := doctor.NewDoctor(repoRoot)
//	d.RegisterCheck(checks.NewURLCheck())
//	d.RegisterCheck(checks.NewIntegrityCheck())
//
// The Check interface is defined in the parent [doctor] package.
package checks
