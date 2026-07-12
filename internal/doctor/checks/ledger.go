package checks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/doctor"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// evidenceStatus is the resolution outcome of a commit evidence reference.
type evidenceStatus int

const (
	evidenceOK          evidenceStatus = iota // repo present and sha resolves
	evidenceRepoMissing                       // repo not present locally (info)
	evidenceShaMissing                        // repo present but sha does not resolve (warning)
)

// commitResolver resolves a commit evidence reference against the local
// checkout. Injected so the check's issue mapping is unit-testable without git.
type commitResolver func(ctx context.Context, campaignRoot, repo, sha string) evidenceStatus

// LedgerCheck reports campaign event-ledger integrity problems (malformed
// lines, unknown envelope versions, duplicate ids, shard-naming violations, and
// dangling evidence refs) through camp's existing doctor surface. It is ledger
// INTEGRITY only; the bypass-scan/reconciliation doctor is a separate surface.
// Findings are informational: nothing here is SeverityError, so a healthy-but-
// noisy ledger never fails `camp doctor`.
type LedgerCheck struct {
	resolve commitResolver
}

// NewLedgerCheck creates the ledger integrity check with git-backed evidence
// resolution.
func NewLedgerCheck() *LedgerCheck {
	return &LedgerCheck{resolve: gitResolveCommit}
}

func (c *LedgerCheck) ID() string   { return "ledger" }
func (c *LedgerCheck) Name() string { return "Ledger Integrity" }
func (c *LedgerCheck) Description() string {
	return "Reports campaign event-ledger integrity issues (malformed lines, unknown versions, duplicate ids, shard naming, dangling evidence)"
}

// Run reads the ledger tolerantly and maps its diagnostics to doctor issues.
func (c *LedgerCheck) Run(ctx context.Context, repoRoot string) (*doctor.CheckResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	reader, err := ledgerkit.NewReader(repoRoot)
	if err != nil {
		return nil, err
	}
	diag, err := reader.Diagnose(ctx)
	if err != nil {
		return nil, err
	}
	issues := c.buildIssues(ctx, repoRoot, diag)
	return &doctor.CheckResult{
		Passed:  !anyError(issues), // ledger findings are info/warning; never an error
		Total:   diag.EventCount,
		Issues:  issues,
		Details: map[string]any{"events": diag.EventCount},
	}, nil
}

// Fix is a no-op: ledger integrity findings are informational and repaired (if
// at all) by the reconciliation/repair surface, not by camp doctor.
func (c *LedgerCheck) Fix(ctx context.Context, repoRoot string, issues []doctor.Issue) ([]doctor.Issue, error) {
	return nil, nil
}

// buildIssues maps ledger diagnostics to doctor issues. It is pure given the
// injected resolver, so every branch is unit-testable without disk or git.
func (c *LedgerCheck) buildIssues(ctx context.Context, campaignRoot string, diag *ledgerkit.Diagnostics) []doctor.Issue {
	var issues []doctor.Issue

	for _, s := range diag.Skipped {
		issues = append(issues, doctor.Issue{
			Severity:    doctor.SeverityWarning,
			CheckID:     c.ID(),
			Description: fmt.Sprintf("malformed or truncated ledger line at %s:%d (%s)", s.Shard, s.Line, s.Reason),
			Details:     map[string]any{"shard": s.Shard, "line": s.Line, "reason": s.Reason, "type": "malformed_line"},
		})
	}

	if diag.UnknownVersions > 0 {
		// A3: an older binary must not scream about a newer ledger. Info only.
		issues = append(issues, doctor.Issue{
			Severity:    doctor.SeverityInfo,
			CheckID:     c.ID(),
			Description: fmt.Sprintf("%d ledger event(s) use a newer envelope version than this camp build understands; upgrade camp to read them fully", diag.UnknownVersions),
			Details:     map[string]any{"count": diag.UnknownVersions, "type": "unknown_version"},
		})
	}

	for _, d := range diag.Duplicates {
		issues = append(issues, doctor.Issue{
			Severity:    doctor.SeverityWarning,
			CheckID:     c.ID(),
			Description: fmt.Sprintf("duplicate event id %s appears %d times", d.ID, len(d.Locations)),
			Details:     map[string]any{"id": d.ID, "locations": d.Locations, "type": "duplicate_id"},
		})
	}

	for _, v := range diag.ShardViolations {
		issues = append(issues, doctor.Issue{
			Severity:    doctor.SeverityWarning,
			CheckID:     c.ID(),
			Description: fmt.Sprintf("ledger shard %s violates the naming scheme: %s", v.Shard, v.Reason),
			Details:     map[string]any{"shard": v.Shard, "reason": v.Reason, "type": "shard_naming"},
		})
	}

	issues = append(issues, c.evidenceIssues(ctx, campaignRoot, diag.Events)...)
	return issues
}

// evidenceIssues resolves each commit evidence reference: a repo absent locally
// is informational (that checkout is not here), while a sha that does not
// resolve in a present repo is a warning (a dangling reference).
func (c *LedgerCheck) evidenceIssues(ctx context.Context, campaignRoot string, events []*ledgerkit.Event) []doctor.Issue {
	var issues []doctor.Issue
	for _, ev := range events {
		for _, e := range ev.Evidence {
			if e.Type != ledgerkit.EvidenceCommit || e.SHA == "" {
				continue
			}
			switch c.resolve(ctx, campaignRoot, e.Repo, e.SHA) {
			case evidenceRepoMissing:
				issues = append(issues, doctor.Issue{
					Severity:    doctor.SeverityInfo,
					CheckID:     c.ID(),
					Description: fmt.Sprintf("evidence repo %q for event %s is not present locally; cannot verify %s", e.Repo, ev.ID, shortSHA(e.SHA)),
					Details:     map[string]any{"event": ev.ID, "repo": e.Repo, "sha": e.SHA, "type": "evidence_repo_missing"},
				})
			case evidenceShaMissing:
				issues = append(issues, doctor.Issue{
					Severity:    doctor.SeverityWarning,
					CheckID:     c.ID(),
					Description: fmt.Sprintf("dangling evidence: commit %s referenced by event %s does not resolve in repo %q", shortSHA(e.SHA), ev.ID, e.Repo),
					Details:     map[string]any{"event": ev.ID, "repo": e.Repo, "sha": e.SHA, "type": "evidence_dangling"},
				})
			}
		}
	}
	return issues
}

// resolveRepoPath maps an evidence repo label to a local path. Labels match
// ledger.RepoLabel:
//   - "campaign-root", "." and "" → campaign root
//   - labels containing "/" (e.g. "projects/camp") → campaignRoot-relative path
//   - bare names (legacy) → projects/<label>
func resolveRepoPath(campaignRoot, repo string) string {
	if repo == "" || repo == "campaign-root" || repo == "." {
		return campaignRoot
	}
	if strings.Contains(repo, "/") || strings.Contains(repo, string(filepath.Separator)) {
		return filepath.Join(campaignRoot, filepath.FromSlash(repo))
	}
	return filepath.Join(campaignRoot, "projects", repo)
}

// gitResolveCommit maps an evidence repo label to a local path and checks the
// sha exists there.
func gitResolveCommit(ctx context.Context, campaignRoot, repo, sha string) evidenceStatus {
	repoPath := resolveRepoPath(campaignRoot, repo)
	if info, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil || (info != nil && !info.IsDir() && !info.Mode().IsRegular()) {
		return evidenceRepoMissing
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "cat-file", "-e", sha+"^{commit}")
	if err := cmd.Run(); err != nil {
		return evidenceShaMissing
	}
	return evidenceOK
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func anyError(issues []doctor.Issue) bool {
	for _, i := range issues {
		if i.Severity == doctor.SeverityError {
			return true
		}
	}
	return false
}
