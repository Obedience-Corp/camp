package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every command that reads or writes git history must drain the deferred queue
// first. That rule was documented in the design and enforced nowhere, so three
// surfaces were missing it: `camp worktrees commit` never drained at all,
// `camp fresh all` ran its own RunE and could not even parse the parent's
// --no-drain, and `commitkit.CommitAll` bypassed the drain its neighbours had.
//
// This test reads the source rather than running the commands. A behavioural
// version would need a real campaign, a real queue, and a real worker per
// command, which is slow enough that it would be run rarely and would not stop
// the next surface from being added without a drain. What actually protects the
// invariant is noticing at compile-and-test time that a history-moving
// entrypoint has no drain call in it, and that is a question about the code.

// queueDuty is what a surface owes the deferred queue.
type queueDuty int

const (
	// mustDrain is for commands that change or publish history. They wait,
	// because a queued commit landing in the middle of what they do, or being
	// left behind by it, is invisible to the user.
	mustDrain queueDuty = iota
	// mustNotice is for commands that only report. They do not wait: holding
	// a user's terminal to make `camp status` marginally fresher is a bad
	// trade against telling them what is still queued and letting them re-run.
	// They still have to say so, or a report silently omits work camp has
	// already promised, which is the confusion deferral must not create.
	mustNotice
	// mustDoBoth is for a command whose duty depends on what it was asked to
	// do. Only doctor: it reports by default and repairs under --fix, so it
	// owes a notice on one path and a wait on the other. Requiring only the
	// wait would let someone make it unconditional again and put a wait back
	// on every doctor; requiring only the notice would let the repair path
	// lose its wait. Both strings have to be there.
	mustDoBoth
)

// drainMatrixEntry is one surface and where its queue handling lives.
type drainMatrixEntry struct {
	// name is how a failure names the surface, in user terms.
	name string
	// file is the source file that must contain the call.
	file string
	// fn is the function the call must appear inside, so moving it to an
	// unrelated helper in the same file does not satisfy the test.
	fn string
	// duty is what this surface owes: a wait, or an acknowledgement.
	duty queueDuty
}

// The surfaces. Adding a command that reads or writes git history means adding
// a row here; a row that does neither of its duties fails, which is the point.
var drainMatrix = []drainMatrixEntry{
	{name: "camp commit", file: "commit.go", fn: "runCommit", duty: mustDrain},
	{name: "camp stage", file: "stage.go", fn: "runStage", duty: mustDrain},
	{name: "camp push", file: "push.go", fn: "runPush", duty: mustDrain},
	{name: "camp push all", file: "push_all.go", fn: "runPushAllCmd", duty: mustDrain},
	{name: "camp pull", file: "pull.go", fn: "runPull", duty: mustDrain},
	{name: "camp pull all", file: "pull_all.go", fn: "runPullAllCmd", duty: mustDrain},
	{name: "camp sync", file: "sync.go", fn: "runSync", duty: mustDrain},
	// doctor --fix repairs a tree, so it waits; plain doctor reports and does
	// not. The wait follows what the command does, not which command it is.
	{name: "camp doctor", file: "doctor.go", fn: "runDoctor", duty: mustDoBoth},
	{name: "camp p commit", file: "project/commit.go", fn: "runProjectCommit", duty: mustDrain},
	{name: "camp worktrees commit", file: "worktrees/commit.go", fn: "runWorktreesCommit", duty: mustDrain},
	{name: "camp refs sync", file: "refs/commands.go", fn: "runRefsSync", duty: mustDrain},
	{name: "camp status", file: "status.go", fn: "runStatus", duty: mustNotice},
	{name: "camp log", file: "log.go", fn: "runLog", duty: mustNotice},
}

// drainCalls are the entrypoints that wait for the queue.
var drainCalls = []string{"drain.Repo(", "drain.AllLanes(", "drain.CampaignRoot("}

// noticeCalls are the entrypoints that report the queue without waiting.
var noticeCalls = []string{"drain.Note(", "drain.NoteAllLanes("}

func TestEveryHistoryMovingCommandDrains(t *testing.T) {
	t.Parallel()

	for _, entry := range drainMatrix {
		t.Run(entry.name, func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(entry.file)
			if err != nil {
				t.Fatalf("read %s: %v", entry.file, err)
			}
			body, ok := functionBody(string(src), entry.fn)
			if !ok {
				t.Fatalf("%s: function %s not found; the matrix is stale",
					entry.file, entry.fn)
			}
			switch entry.duty {
			case mustDrain:
				if !containsAny(body, drainCalls) {
					t.Errorf("%s moves git history but never drains the deferred queue.\n"+
						"A queued commit can then land in the middle of what it does, or be "+
						"left behind by it.\nAdd a drain in %s (%s), or remove the row from "+
						"drainMatrix if this command no longer touches history.",
						entry.name, entry.fn, entry.file)
				}
			case mustNotice:
				if !containsAny(body, noticeCalls) {
					t.Errorf("%s reports git history but never mentions the deferred queue.\n"+
						"It would then omit a commit camp has already promised, with nothing "+
						"on screen to explain the gap.\nAdd a drain.Note in %s (%s).",
						entry.name, entry.fn, entry.file)
				}
				if containsAny(body, drainCalls) {
					t.Errorf("%s only reports, so it must not wait for the queue.\n"+
						"Holding the terminal to make a report marginally fresher is the "+
						"cost deferral exists to remove; %s (%s) should call drain.Note.",
						entry.name, entry.fn, entry.file)
				}
			case mustDoBoth:
				if !containsAny(body, drainCalls) {
					t.Errorf("%s repairs under --fix but never waits for the queue.\n"+
						"It would repair against a tree camp is still changing.\n"+
						"Add a blocking drain on the writing path in %s (%s).",
						entry.name, entry.fn, entry.file)
				}
				if !containsAny(body, noticeCalls) {
					t.Errorf("%s reports by default but never mentions the deferred queue.\n"+
						"Either the notice was dropped, or the wait became unconditional and "+
						"every %s now pays for the queue again.\n"+
						"Both paths must be present in %s (%s).",
						entry.name, entry.name, entry.fn, entry.file)
				}
			}
		})
	}
}

// The fest-facing surface has the same requirement, and its own trap: which of
// these a consumer calls is its business, so the guarantee cannot depend on the
// choice. CommitAll was the one that did not drain.
func TestCommitkitEntrypointsDrain(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("..", "..", "pkg", "commitkit", "commitkit.go"))
	if err != nil {
		t.Fatalf("read commitkit.go: %v", err)
	}
	for _, fn := range []string{
		"StageAllWithOutcome",
		"StageFilesWithOutcome",
		"Commit",
		"CommitAll",
	} {
		body, ok := functionBody(string(src), fn)
		if !ok {
			t.Errorf("commitkit.%s not found; the list is stale", fn)
			continue
		}
		if !strings.Contains(body, "drainQuietly(") {
			t.Errorf("commitkit.%s does not drain.\n"+
				"A consumer that happens to use this entrypoint would commit on top of a "+
				"tree camp is still about to change, and which function it calls is not "+
				"something camp gets to assume.", fn)
		}
	}
}

// functionBody returns the source text of a top-level function, from its
// signature to the closing brace at column zero.
func functionBody(src, name string) (string, bool) {
	marker := "\nfunc " + name + "("
	start := strings.Index(src, marker)
	if start < 0 {
		// Methods and multi-line signatures: fall back to a looser anchor.
		marker = "\nfunc " + name + "\n"
		start = strings.Index(src, marker)
		if start < 0 {
			return "", false
		}
	}
	rest := src[start+1:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		return rest, true
	}
	return rest[:end], true
}

// The matrix is only as good as its coverage, so this asserts the harness can
// actually read what it claims to: a row naming a function that does not exist
// must fail loudly rather than silently pass.
func TestFunctionBodyFindsAndMisses(t *testing.T) {
	t.Parallel()

	const src = "package x\n\nfunc alpha(ctx context.Context) error {\n\tdrain.Repo()\n\treturn nil\n}\n\nfunc beta() {}\n"

	body, ok := functionBody(src, "alpha")
	if !ok || !strings.Contains(body, "drain.Repo()") {
		t.Errorf("functionBody did not return alpha's body: ok=%v body=%q", ok, body)
	}
	if _, ok := functionBody(src, "gamma"); ok {
		t.Error("functionBody claimed to find a function that does not exist")
	}
}

// A guard against the matrix silently shrinking: the drain surfaces are spread
// across three packages, and `go vet` will not notice a deleted row.
func TestDrainMatrixCoversEveryPackageWithADrain(t *testing.T) {
	t.Parallel()

	out, err := exec.Command("grep", "-rl", "drain.Repo(\\|drain.AllLanes(\\|drain.CampaignRoot(",
		".", "../../internal/commands").Output()
	if err != nil {
		t.Skipf("grep unavailable: %v", err)
	}

	covered := make(map[string]bool, len(drainMatrix))
	for _, e := range drainMatrix {
		covered[filepath.Base(e.file)] = true
	}
	// jobs.go is the queue's own surface (`camp jobs drain`), not a
	// history-moving command, so it is expected to drain without a row.
	covered["jobs.go"] = true
	// fresh lives in internal/commands and has its own tests; the matrix reads
	// cmd/camp only. Its batch helper owns the target-scoped drains shared by
	// the single-project and all-project entrypoints.
	covered["fresh.go"] = true
	covered["all.go"] = true
	covered["batch.go"] = true

	for _, path := range strings.Fields(string(out)) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		if !covered[filepath.Base(path)] {
			t.Errorf("%s calls a drain but has no row in drainMatrix; "+
				"add one so the call cannot be removed unnoticed", path)
		}
	}
}

// containsAny reports whether src holds any of the given call sites.
func containsAny(src string, calls []string) bool {
	for _, call := range calls {
		if strings.Contains(src, call) {
			return true
		}
	}
	return false
}
