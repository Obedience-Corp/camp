package cmdutil

import (
	"bytes"
	"context"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// --commit-large is the user having already answered the question, so the pass
// must not even ask git. A repository path that cannot be read proves it
// returned before running anything.
func TestReportStagedGrowthCommitLargeSuppressesEverything(t *testing.T) {
	var out bytes.Buffer

	violations := ReportStagedGrowth(context.Background(), &out, "/nonexistent/repo", true)

	if len(violations) != 0 {
		t.Errorf("ReportStagedGrowth() = %+v, want none under --commit-large", violations)
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want nothing under --commit-large", out.String())
	}
}

// Fail-open, but never quietly: a repository camp cannot read still commits,
// and the user is told the check did not run. The silent version is the
// dangerous one, because it leaves them believing they were checked.
func TestReportStagedGrowthFailsOpenAndSaysSo(t *testing.T) {
	var out bytes.Buffer

	// An empty directory is not a git repository, so the diff fails the way a
	// broken or mid-scaffold repository would.
	violations := ReportStagedGrowth(context.Background(), &out, t.TempDir(), false)

	if len(violations) != 0 {
		t.Errorf("ReportStagedGrowth() = %+v, want none when the check cannot run", violations)
	}
	if !strings.Contains(out.String(), "Could not run the size check") {
		t.Errorf("output = %q, want it to say the check did not run", out.String())
	}
}

func TestStagedCheckUnavailableLineNamesTheCause(t *testing.T) {
	line := StagedCheckUnavailableLine(camperrors.New("git exploded"))

	if !strings.Contains(line, "git exploded") {
		t.Errorf("line = %q, want the cause named", line)
	}
	// A user told only that a check was skipped has nothing to do about it.
	if !strings.Contains(line, "camp doctor -c bigfiles") {
		t.Errorf("line = %q, want a way to look for large files", line)
	}
}

// The line prints before the commit object exists, and the flow after it can
// still end in "nothing to commit", a refusal over pre-staged submodule refs, a
// deferred message writer, or a git failure. Past tense would assert an outcome
// camp does not know yet, which is the same wrong-by-assertion problem the line
// exists to avoid.
func TestStagedCheckUnavailableLineClaimsNoCommit(t *testing.T) {
	line := StagedCheckUnavailableLine(camperrors.New("git exploded"))

	for _, claim := range []string{"Committed", "committed", "Commit succeeded"} {
		if strings.Contains(line, claim) {
			t.Errorf("line = %q, must not claim a commit happened (found %q)", line, claim)
		}
	}
}
