package ledger

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// RepoLabel is the campaign-relative label the ledger uses for a committed
// repo: "campaign-root" for the campaign root itself, otherwise the path
// relative to the campaign root (e.g. "projects/camp"). This is the same
// convention camp doctor's dangling-evidence check resolves against, so the two
// agree on what evidence.Repo means.
func RepoLabel(campaignRoot, repoPath string) string {
	if repoPath == "" || repoPath == campaignRoot {
		return "campaign-root"
	}
	if rel, err := filepath.Rel(campaignRoot, repoPath); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return "campaign-root"
}

// CommitEvidence appends an evidence_attached event for a commit that landed at
// sha in repoPath. All commits from one wrapper invocation share the emitter's
// action id (D002 action identity), so a root commit plus its submodule commits
// group as one logical action. Tagging and the commit message are untouched;
// this only records the commit as evidence (D003 ordering: evidence after the
// commit exists).
//
// When git can resolve the commit, the event timestamp is the author-date and
// payload carries author + commit_date so ledger readers (e.g. workitem commits)
// match git-log shape rather than capture wall-clock.
func (e *Emitter) CommitEvidence(ctx context.Context, scope ledgerkit.Scope, campaignRoot, repoPath, sha, subject string) {
	label := RepoLabel(campaignRoot, repoPath)
	opts := []Option{
		WithEvidence(ledgerkit.Evidence{
			Type: ledgerkit.EvidenceCommit,
			Repo: label,
			SHA:  sha,
		}),
		WithWhy(firstLine(subject)),
	}
	if author, date, ok := commitAuthorDate(ctx, repoPath, sha); ok {
		opts = append(opts,
			WithTS(date),
			WithPayload(map[string]any{"author": author, "commit_date": date}),
		)
	}
	e.Emit(ctx, ledgerkit.KindEvidenceAttached, scope, opts...)
}

// commitAuthorDate resolves git author name and author-date (RFC3339) for sha.
// Best-effort: returns ok=false when git is unavailable or the object is missing.
func commitAuthorDate(ctx context.Context, repoPath, sha string) (author, date string, ok bool) {
	if repoPath == "" || sha == "" {
		return "", "", false
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "show", "-s", "--format=%aN%x1f%aI", sha)
	out, err := cmd.Output()
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\x1f", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	// Normalize to UTC RFC3339 so ledger and workitem commits share one parse.
	if t, err := time.Parse(time.RFC3339, parts[1]); err == nil {
		return parts[0], ledgerkit.NowUTC(t), true
	}
	return parts[0], parts[1], true
}

// firstLine returns the first line of a (possibly tagged) commit message, for a
// concise why.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
