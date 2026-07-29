// Package jobs is camp's durable deferred-commit queue.
//
// The queue exists so bookkeeping commits stop holding the user's terminal.
// Its design constraints all follow from one property: a job is a promise camp
// made to write something to git later, so losing one silently is the worst
// possible failure. Everything here is therefore crash-safe by construction
// rather than by cleanup.
//
// One file per job, never an append log. Enqueue is O_CREATE|O_EXCL, claim is
// rename, completion is unlink, failure is rename. Each is a single atomic
// filesystem operation on POSIX, so a process dying at any point leaves the
// queue in a state the next worker can read correctly. A JSONL log would need
// rewriting to mark completion, with partial-line risk on a crash.
//
// The queue lives under .campaign/cache because it is derived, machine-local,
// gitignored, and disposable, the same treatment manifests and sync snapshots
// get. Nothing here is the record of anything; git is.
package jobs

import (
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Kind distinguishes what a job snapshots. Conflating the two would be a
// correctness bug, not a stylistic one: they capture the work at different
// moments and by different means.
type Kind string

const (
	// KindCommitPaths is camp's own bookkeeping: intent capture, workitem
	// changes, dungeon moves, manifests. The content is already on disk, so
	// the named paths are the whole snapshot, and the worker stages them
	// through camp's temp-index path without touching the real index.
	KindCommitPaths Kind = "commit-paths"
	// KindCommitTree is the user's own `-aw` commit. Staging already happened
	// in the foreground, so the real index holds the snapshot; the enqueuer
	// captures it as a tree object and the worker commits that exact tree.
	// Running a plain `git commit` later would sweep in anything staged in the
	// meantime by another terminal.
	KindCommitTree Kind = "commit-tree"
)

// Valid reports whether k is a kind this package executes.
func (k Kind) Valid() bool {
	return k == KindCommitPaths || k == KindCommitTree
}

// Class says whether a drain waits for a job.
//
// It is a separate axis from Kind because the two answer different questions:
// Kind is how the worker executes the job, Class is whether anyone downstream
// depends on it having executed. A manifest and an intent capture are both
// commit-paths jobs, but one is a record that is correct whenever it lands and
// the other is content a later push must not miss.
type Class string

const (
	// ClassCommit is ordinary deferred work: a drain waits for it, because a
	// command that reads or writes history would otherwise see a state camp
	// has already promised to change.
	ClassCommit Class = "commit"
	// ClassManifest is an artifact manifest, exempt from every drain.
	//
	// A manifest carries describes_commit, so it is correct whichever commit
	// eventually holds it and has no ordering relationship to anything. The
	// exemption is what makes hashing inside the worker safe at all: without
	// it, a first-pass hash of a large artifact root would leak straight back
	// onto the user's critical path through the next drain, which is the
	// latency this subsystem exists to remove.
	ClassManifest Class = "manifest"
)

// Blocking reports whether a drain waits for this class.
//
// Everything except manifest blocks, including the empty string and any value
// this build does not recognize. That direction is deliberate: mistaking a
// blocking job for an exempt one silently breaks the ordering barrier and the
// user sees a push that is missing a commit, while mistaking an exempt job for
// a blocking one costs a wait. Only the cheap mistake is available by default.
func (c Class) Blocking() bool { return c != ClassManifest }

// validClass reports whether c is a class this build assigns meaning to.
// Enqueue rejects anything else so a typo cannot quietly become "blocking",
// which would look like ordinary slowness rather than a bug.
func validClass(c Class) bool {
	return c == "" || c == ClassCommit || c == ClassManifest
}

// Job is one unit of deferred work. Exactly one lives in each queue file.
//
// There is deliberately no state field. State is which directory the file is
// in, because a rename between directories is atomic while a read-modify-write
// of a field is not.
type Job struct {
	// ID is the human-readable identifier, also the filename stem.
	ID string `json:"id"`
	// Seq orders execution within a lane. Filenames lead with it zero-padded,
	// so a lexical directory sort is the execution order.
	Seq int `json:"seq"`
	// Kind selects how the worker executes this job.
	Kind Kind `json:"kind"`
	// Class says whether a drain waits for this job. Empty means commit, so
	// omitting the field is the safe default rather than the fast one.
	Class Class `json:"class,omitempty"`
	// Repo is campaign-relative: "." is the campaign root, "projects/camp" a
	// submodule. Jobs never span repos.
	Repo string `json:"repo"`
	// Paths are repo-relative and explicit, for KindCommitPaths. There is no
	// glob, no ".", and no "everything" form: a deferred job that stages
	// whatever it finds later would commit work the user never associated with
	// it.
	Paths []string `json:"paths,omitempty"`
	// Blobs is the captured content of Paths, for KindCommitPaths.
	//
	// Recorded at enqueue so the job's effect is a function of enqueue time
	// only. Paths alone defer the read as well as the write: an edit made
	// between the two lands under the earlier operation's message, so renaming
	// a workitem and then editing it puts the edit in the rename's commit.
	//
	// Empty is tolerated for hand-written and older jobs, which fall back to
	// reading the paths at execution.
	Blobs []BlobRef `json:"blobs,omitempty"`
	// Tree is the captured tree SHA, for KindCommitTree.
	Tree string `json:"tree,omitempty"`
	// Parent is the HEAD the tree was captured against. A mismatch at
	// execution time fails the job rather than guessing.
	Parent string `json:"parent,omitempty"`
	// Message is the commit message. Empty with AutoWrite set means the worker
	// generates it.
	Message string `json:"message,omitempty"`
	// AutoWrite makes the worker run the configured message writer against the
	// captured tree rather than the live worktree.
	AutoWrite bool `json:"auto_write,omitempty"`
	// MessagePrefix is prepended to whatever the writer produces.
	//
	// The campaign tag is resolved at enqueue and carried here because it
	// depends on the enqueuing process's working directory: the campaign, the
	// quest, the festival, and the workitem all come from where the user ran
	// the command. A detached worker has none of that, so a deferred commit
	// that resolved its own tag would silently produce an untagged commit
	// where the synchronous one is tagged.
	MessagePrefix string `json:"message_prefix,omitempty"`
	// Env carries CAMP_* variables the message writer needs, as "KEY=VALUE".
	//
	// It has to be recorded rather than re-derived. The workitem context comes
	// from the enqueuing process's working directory, and the worker is a
	// detached child with no meaningful cwd, so anything not captured here is
	// simply lost: a deferred commit would silently drop the workitem tag that
	// the same commit made in the foreground would have carried.
	Env []string `json:"env,omitempty"`
	// Then is an optional follow-up the worker enqueues after this job lands.
	// One level only; a follow-up may not carry its own.
	Then *Follow `json:"then,omitempty"`
	// FollowUp marks a job the worker created from another job's Then.
	//
	// It exists for one carve-out. Deferred jobs may not commit submodule
	// pointers, because a gitlink records whatever the submodule's HEAD
	// happens to be at execution time, which is not a snapshot the enqueuer
	// chose. The single exception is this follow-up: it runs precisely because
	// its parent just moved that submodule's HEAD, so the pointer it records is
	// the commit the parent made. Narrow on purpose, and enforced at execution
	// rather than by convention.
	FollowUp bool `json:"follow_up,omitempty"`
	// CreatedAt is the enqueuing process's clock, RFC3339 with millis.
	CreatedAt string `json:"created_at"`
	// Attempts counts how many times this job has been claimed.
	Attempts int `json:"attempts"`
}

// BlobRef is one captured path and its content object. It mirrors
// git.BlobRef, which the queue cannot embed directly without making the job
// document depend on that package's field tags.
type BlobRef struct {
	Path string `json:"path"`
	Mode string `json:"mode,omitempty"`
	// SHA is empty when the path was already gone at capture time, which
	// records a deletion.
	SHA string `json:"sha,omitempty"`
}

// Follow is a job the worker enqueues once its parent job lands. The shape is
// restricted to what the one real use needs: recording a submodule gitlink in
// the root repo after a submodule commit.
type Follow struct {
	// Kind is always KindCommitPaths. A follow-up that captured a tree would
	// need a tree that does not exist until its parent has run.
	Kind Kind `json:"kind"`
	// Repo is campaign-relative, "." for the sync follow-up.
	Repo string `json:"repo"`
	// Paths is the gitlink path to record.
	Paths []string `json:"paths"`
	// Message is the commit message the follow-up will carry.
	//
	// Composed by the enqueuer rather than the worker so a deferred follow-up
	// produces the same commit its synchronous twin would. The worker has the
	// campaign root but not the settings context the message depends on, and a
	// message invented here would put the queue's own vocabulary into
	// permanent git history.
	Message string `json:"message,omitempty"`
}

// Validate rejects jobs the worker could not execute correctly.
//
// Enqueue-time validation is the only place these rules can be enforced
// cheaply: once a job file exists it is a promise, and discovering at
// execution time that the promise was malformed means failing work the user
// believes is queued.
func (j *Job) Validate() error {
	if !j.Kind.Valid() {
		return camperrors.NewValidation("kind",
			"must be "+string(KindCommitPaths)+" or "+string(KindCommitTree), nil)
	}
	if !validClass(j.Class) {
		return camperrors.NewValidation("class",
			"must be "+string(ClassCommit)+", "+string(ClassManifest)+", or empty", nil)
	}
	if err := validateEnv(j.Env); err != nil {
		return err
	}
	if err := validateRepo(j.Repo); err != nil {
		return err
	}
	if j.Then != nil {
		if err := j.Then.validate(); err != nil {
			return err
		}
		// Criterion 37m: chaining is bounded at one level. A follow-up that
		// carried its own would let a queue file describe an unbounded chain,
		// and each link runs against a repository further from the state its
		// author saw.
		if j.FollowUp {
			return camperrors.NewValidation("then",
				"a follow-up may not carry its own follow-up", nil)
		}
	}
	if j.FollowUp && len(j.Paths) != 1 {
		return camperrors.NewValidation("paths",
			"a follow-up records exactly one path", nil)
	}

	switch j.Kind {
	case KindCommitPaths:
		return validatePaths(j.Paths)
	case KindCommitTree:
		if strings.TrimSpace(j.Tree) == "" {
			return camperrors.NewValidation("tree", "commit-tree requires a captured tree", nil)
		}
		// Execution refuses a job with no parent, so accepting one here would
		// let a malformed job become a promise camp cannot keep: it would sit
		// in the queue, fail, and be retried to exhaustion for a reason the
		// enqueuer could have been told immediately.
		if strings.TrimSpace(j.Parent) == "" {
			return camperrors.NewValidation("parent",
				"commit-tree requires the HEAD its tree was captured against", nil)
		}
		if strings.TrimSpace(j.Message) == "" && !j.AutoWrite {
			return camperrors.NewValidation("message",
				"commit-tree requires a message or auto_write", nil)
		}
	}
	return nil
}

// validate checks a follow-up. It exists as its own method so the "one level
// only" rule has a single home.
func (f *Follow) validate() error {
	if f.Kind != KindCommitPaths {
		return camperrors.NewValidation("then.kind",
			"a follow-up must be "+string(KindCommitPaths), nil)
	}
	if err := validateRepoField("then.repo", f.Repo); err != nil {
		return err
	}
	if strings.TrimSpace(f.Message) == "" {
		// The worker records what it is given and composes nothing. A
		// follow-up with no message would reach git with an empty subject,
		// which git either rejects or, worse, accepts.
		return camperrors.NewValidation("then.message",
			"a follow-up must carry the message its synchronous twin would use", nil)
	}
	return validatePathsField("then.paths", f.Paths)
}

// envKeyPrefix bounds what a job may put in the writer's environment.
const envKeyPrefix = "CAMP_"

// validateEnv rejects environment entries a job has no business setting.
//
// A job file lives on disk and is hand-editable, and the worker feeds these
// straight into a subprocess. Restricting to CAMP_* keeps a queue file from
// becoming a way to set PATH or LD_PRELOAD for a process the user did not
// start and is not watching. The writer only ever needs camp's own variables.
func validateEnv(env []string) error {
	for _, entry := range env {
		key, _, found := strings.Cut(entry, "=")
		if !found || key == "" {
			return camperrors.NewValidation("env",
				"entry "+entry+" must be KEY=VALUE", nil)
		}
		if !strings.HasPrefix(key, envKeyPrefix) {
			return camperrors.NewValidation("env",
				"variable "+key+" is not a "+envKeyPrefix+"* variable; a queued job may only set camp's own", nil)
		}
	}
	return nil
}

// validateRepo checks a job's repo field.
func validateRepo(repo string) error {
	return validateRepoField("repo", repo)
}

// validateRepoField rejects repo paths that are not campaign-relative.
//
// A repo is a lane, and a lane is a directory name. An absolute or escaping
// path would name a repository outside the campaign, which the worker would
// then commit into: the queue would be writing to a repo the campaign does not
// own. Rejecting at enqueue is the only cheap place to catch it, because by
// execution time the job is already a promise.
func validateRepoField(field, repo string) error {
	trimmed := strings.TrimSpace(repo)
	if trimmed == "" {
		return camperrors.NewValidation(field, "must not be empty", nil)
	}
	if filepath.IsAbs(trimmed) {
		return camperrors.NewValidation(field,
			"repo "+repo+" must be campaign-relative", nil)
	}
	normalized := strings.Trim(filepath.ToSlash(filepath.Clean(trimmed)), "/")
	if normalized == ".." || strings.HasPrefix(normalized, "../") {
		return camperrors.NewValidation(field,
			"repo "+repo+" escapes the campaign", nil)
	}
	return nil
}

// validatePaths enforces the explicit-paths rule for a job.
func validatePaths(paths []string) error {
	return validatePathsField("paths", paths)
}

// validatePathsField rejects empty, wildcard, and whole-tree path lists.
//
// The rule is the queue's central safety property. A deferred job runs at an
// unknown later moment, so a glob or a "." would stage whatever happens to be
// present then, sweeping in work the user never associated with this commit.
// Explicit paths make a job's effect a function of enqueue time only.
func validatePathsField(field string, paths []string) error {
	if len(paths) == 0 {
		return camperrors.NewValidation(field, "requires at least one explicit path", nil)
	}
	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		switch {
		case trimmed == "":
			return camperrors.NewValidation(field, "path must not be empty", nil)
		case trimmed == "." || trimmed == "./" || trimmed == "*":
			return camperrors.NewValidation(field,
				"path "+p+" would stage everything present at execution time; list paths explicitly", nil)
		case strings.ContainsAny(trimmed, "*?["):
			return camperrors.NewValidation(field,
				"path "+p+" looks like a glob; a deferred job must name its paths exactly", nil)
		case strings.HasPrefix(trimmed, "/"):
			return camperrors.NewValidation(field, "path "+p+" must be repo-relative", nil)
		case trimmed == ".." || strings.HasPrefix(trimmed, "../"):
			return camperrors.NewValidation(field, "path "+p+" escapes the repository", nil)
		}
	}
	return nil
}
