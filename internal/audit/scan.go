// Package audit implements the campaign audit doctor (D004): a scan that
// classifies commits across linked repos as tagged/degraded/untagged, a
// reconciliation pass that derives events from state files and fills ledger
// gaps, and opt-in repair. It is informational by design: untagged commits are
// a normal mode for wrapper-opt-out users, not a violation. No git hooks.
package audit

import (
	"bufio"
	"context"
	"os/exec"
	"strconv"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

// CommitClass is how a commit's subject classifies against the campaign tag
// grammar.
type CommitClass string

const (
	// ClassTagged is a recognized leading campaign tag with no shape warnings.
	ClassTagged CommitClass = "tagged"
	// ClassDegraded is a recognized leading tag with >=1 shape-check warning.
	ClassDegraded CommitClass = "degraded"
	// ClassUntagged is a commit with no recognized leading campaign tag: the
	// bypass hole this doctor surfaces informationally.
	ClassUntagged CommitClass = "untagged"
)

// ClassifyCommit classifies a commit subject using the same parser the ledger
// uses (commitkit.ParseTagDetailed). It is pure and unit-testable.
func ClassifyCommit(subject string) CommitClass {
	tc, warnings := commitkit.ParseTagDetailed(subject)
	switch {
	case tc.CampaignID == "":
		return ClassUntagged
	case len(warnings) > 0:
		return ClassDegraded
	default:
		return ClassTagged
	}
}

// UntaggedCommit is one commit with no captured intent linkage.
type UntaggedCommit struct {
	SHA     string
	Author  string
	Subject string
}

// RepoScan is the classification result for one repo.
type RepoScan struct {
	Repo     string // campaign-relative label
	Tagged   int
	Degraded int
	Untagged int
	Total    int
	// Sample holds up to SampleLimit untagged commits so the report can show the
	// bypass population without dumping thousands of lines.
	Sample []UntaggedCommit
}

// SampleLimit bounds the untagged sample per repo in a scan report.
const SampleLimit = 20

// gitLogLine is the record separator layout used by ScanRepo.
const gitLogFormat = "%H%x1f%an%x1f%s"

// ScanRepo walks a repo's history (optionally bounded to the most recent
// maxCommits, 0 = all) and classifies every non-merge commit. It shells out to
// git read-only; a repo with no commits yields a zero scan, not an error.
func ScanRepo(ctx context.Context, repoLabel, repoPath string, maxCommits int) (RepoScan, error) {
	if err := ctx.Err(); err != nil {
		return RepoScan{}, err
	}
	args := []string{"-C", repoPath, "log", "--no-merges", "--format=" + gitLogFormat}
	if maxCommits > 0 {
		args = append(args, "-n", strconv.Itoa(maxCommits))
	}
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		// An empty/uninitialized repo (no HEAD) is not a scan error.
		if isNoHeadErr(err) {
			return RepoScan{Repo: repoLabel}, nil
		}
		return RepoScan{}, camperrors.Wrapf(err, "audit: git log in %s", repoPath)
	}
	scan := RepoScan{Repo: repoLabel}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 3)
		if len(parts) != 3 {
			continue
		}
		sha, author, subject := parts[0], parts[1], parts[2]
		scan.Total++
		switch ClassifyCommit(subject) {
		case ClassTagged:
			scan.Tagged++
		case ClassDegraded:
			scan.Degraded++
		case ClassUntagged:
			scan.Untagged++
			if len(scan.Sample) < SampleLimit {
				scan.Sample = append(scan.Sample, UntaggedCommit{SHA: sha, Author: author, Subject: subject})
			}
		}
	}
	return scan, sc.Err()
}

func isNoHeadErr(err error) bool {
	var exitErr *exec.ExitError
	if strings.Contains(err.Error(), "does not have any commits") {
		return true
	}
	// git log on an unborn branch exits 128 with a stderr message; Output()
	// discards stderr, so fall back to the exit code plus a best-effort check.
	if e, ok := asExit(err, &exitErr); ok && e == 128 {
		return true
	}
	return false
}

// asExit extracts the exit code from an error if it is an *exec.ExitError.
func asExit(err error, target **exec.ExitError) (int, bool) {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return ee.ExitCode(), true
	}
	return 0, false
}
