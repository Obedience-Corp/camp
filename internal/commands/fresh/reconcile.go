package fresh

import (
	"context"
	"fmt"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
)

const (
	freshRecoveryPrefix = "camp-fresh-recovery-"
	freshRecoverySHALen = 12
)

// fetchFreshRemote refreshes origin once for both default-branch sync and the
// later prune pass. A dry-run intentionally refreshes remote-tracking refs so
// its plan is based on current remote truth; it still does not move local refs.
func fetchFreshRemote(ctx context.Context, path string, pruneEnabled bool) error {
	if pruneEnabled {
		return git.FetchRemotePrune(ctx, path, "origin")
	}
	return git.FetchRemote(ctx, path, "origin")
}

// reconcileFreshDefault makes a checked-out local default branch match its
// fetched origin ref. Local-only commits are first anchored by a recovery
// branch, which makes the subsequent hard reset lossless and reversible.
func reconcileFreshDefault(ctx context.Context, path, defaultBranch string, dryRun bool) error {
	localRef := "refs/heads/" + defaultBranch
	remoteRef := "refs/remotes/origin/" + defaultBranch

	if _, err := git.Output(ctx, path, "rev-parse", "--verify", remoteRef); err != nil {
		return camperrors.Wrapf(err, "verify %s", remoteRef)
	}

	divergence, err := git.CompareRefs(ctx, path, localRef, remoteRef)
	if err != nil {
		return camperrors.Wrap(err, "compare local and remote default branches")
	}

	syncLabel := defaultBranch + " ← origin/" + defaultBranch
	if divergence.Ahead == 0 {
		if divergence.Behind == 0 {
			freshRow("Sync "+syncLabel, "up-to-date", ui.StatusMuted)
			return nil
		}
		if dryRun {
			freshRow("Would fast-forward "+defaultBranch,
				fmt.Sprintf("%d commit(s) from origin/%s", divergence.Behind, defaultBranch), ui.StatusMuted)
			return nil
		}
		if err := git.FastForwardTo(ctx, path, remoteRef); err != nil {
			return camperrors.Wrapf(err, "fast-forward %s to origin/%s", defaultBranch, defaultBranch)
		}
		freshRow("Sync "+syncLabel, fmt.Sprintf("updated %d commit(s)", divergence.Behind), ui.StatusSuccess)
		return nil
	}

	oldTip, err := git.Output(ctx, path, "rev-parse", "--verify", localRef)
	if err != nil {
		return camperrors.Wrapf(err, "resolve local %s tip", defaultBranch)
	}
	recoveryBranch, reuse, err := selectFreshRecoveryBranch(ctx, path, oldTip)
	if err != nil {
		return err
	}

	if dryRun {
		action := "Would preserve"
		if reuse {
			action = "Recovery exists"
		}
		freshRow(action+" "+recoveryBranch, fmt.Sprintf("%d local-only commit(s)", divergence.Ahead), ui.StatusMuted)
		freshRow("Would realign "+defaultBranch, "to origin/"+defaultBranch, ui.StatusMuted)
		return nil
	}

	if !reuse {
		if err := git.CreateBranchRef(ctx, path, recoveryBranch, oldTip); err != nil {
			return camperrors.Wrapf(err, "preserve local %s at %s", defaultBranch, recoveryBranch)
		}
	}
	status := "done"
	tone := ui.StatusSuccess
	if reuse {
		status = "already preserved"
		tone = ui.StatusMuted
	}
	freshRow("Preserve "+recoveryBranch, status, tone)
	freshDetail(fmt.Sprintf("%d local-only commit(s)", divergence.Ahead))

	if err := git.ResetHardTo(ctx, path, remoteRef); err != nil {
		return camperrors.Wrapf(err,
			"realign %s to origin/%s; local commits remain at %s", defaultBranch, defaultBranch, recoveryBranch)
	}
	freshRow("Realign "+defaultBranch+" -> origin/"+defaultBranch, "done", ui.StatusSuccess)
	freshDetail(freshRecoveryUndo(defaultBranch, recoveryBranch))
	return nil
}

// selectFreshRecoveryBranch returns a deterministic available recovery branch.
// If the base candidate already points to oldTip it is reused, making retries
// idempotent. A conflicting candidate receives a numeric suffix.
func selectFreshRecoveryBranch(ctx context.Context, path, oldTip string) (branch string, reuse bool, err error) {
	base := freshRecoveryBranchBase(oldTip)
	for suffix := 1; suffix <= 1000; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		if !git.BranchExists(ctx, path, candidate) {
			return candidate, false, nil
		}
		tip, resolveErr := git.Output(ctx, path, "rev-parse", "--verify", "refs/heads/"+candidate)
		if resolveErr != nil {
			return "", false, camperrors.Wrapf(resolveErr, "resolve recovery branch %s", candidate)
		}
		if strings.TrimSpace(tip) == strings.TrimSpace(oldTip) {
			return candidate, true, nil
		}
	}
	return "", false, camperrors.New("could not allocate a recovery branch after 1000 attempts")
}

func freshRecoveryBranchBase(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > freshRecoverySHALen {
		sha = sha[:freshRecoverySHALen]
	}
	return freshRecoveryPrefix + sha
}

func freshRecoveryUndo(defaultBranch, recoveryBranch string) string {
	return fmt.Sprintf("Undo: git switch %s && git reset --hard %s", defaultBranch, recoveryBranch)
}
