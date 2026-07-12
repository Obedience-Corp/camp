package ledger

import (
	"context"
	"path/filepath"
	"strings"

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
func (e *Emitter) CommitEvidence(ctx context.Context, scope ledgerkit.Scope, campaignRoot, repoPath, sha, subject string) {
	e.Emit(ctx, ledgerkit.KindEvidenceAttached, scope,
		WithEvidence(ledgerkit.Evidence{
			Type: ledgerkit.EvidenceCommit,
			Repo: RepoLabel(campaignRoot, repoPath),
			SHA:  sha,
		}),
		WithWhy(firstLine(subject)))
}

// firstLine returns the first line of a (possibly tagged) commit message, for a
// concise why.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
