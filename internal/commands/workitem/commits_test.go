package workitem

import (
	"bytes"
	"testing"
)

func TestCommitsWorkerCountCapsFanout(t *testing.T) {
	for _, repoCount := range []int{0, 1, 2, 100} {
		got := commitsWorkerCount(repoCount)
		if got < 1 {
			t.Fatalf("commitsWorkerCount(%d) = %d, want >= 1", repoCount, got)
		}
		if got > commitsMaxWorkers {
			t.Fatalf("commitsWorkerCount(%d) = %d, want <= %d", repoCount, got, commitsMaxWorkers)
		}
		if repoCount > 0 && got > repoCount {
			t.Fatalf("commitsWorkerCount(%d) = %d, want <= repo count", repoCount, got)
		}
	}
}

func TestEmitCommitsQueryWarnings(t *testing.T) {
	var stderr bytes.Buffer
	if err := emitCommitsQueryWarnings(&stderr, []commitsQueryError{{Repo: "demo", Err: "boom"}}); err != nil {
		t.Fatal(err)
	}
	if got := stderr.String(); got != "warning: 1 repo(s) failed; re-run with --json for details\n" {
		t.Fatalf("warning = %q", got)
	}

	stderr.Reset()
	if err := emitCommitsQueryWarnings(&stderr, nil); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("empty errors emitted warning: %q", stderr.String())
	}
}
