// Package defercommit decides whether an --auto-write commit defers, and
// enqueues it when it does.
//
// It exists so the three commit commands (camp commit, camp p commit,
// camp worktrees commit) cannot answer that question differently. The decision
// has five reasons to say no and each one is a correctness rule rather than a
// preference, so a copy of this logic that drifted by one condition would
// silently either skip a user's git hook or break the synchronous contract
// `--json` consumers depend on.
package defercommit

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/Obedience-Corp/camp/internal/autowrite"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/jobs"
)

// EnvNoDefer forces every deferred commit to run inline.
//
// Test harnesses and agents that need strict determinism set it and get exactly
// the behavior camp had before deferral existed. It is the honest escape hatch:
// deferral changes when a commit exists, and a caller that cannot tolerate that
// should be able to turn it off without also turning off --auto-write.
const EnvNoDefer = "CAMP_NO_DEFER"

// Refusal says why a commit will not defer, for the caller to report.
type Refusal string

const (
	// RefusedDisabled is CAMP_NO_DEFER=1.
	RefusedDisabled Refusal = "CAMP_NO_DEFER is set"
	// RefusedHooks is a repository with commit hooks. A hook is the user's own
	// code expecting to run at commit time, against the tree being committed.
	// Deferring past it would either skip it or run it minutes later against a
	// different working tree.
	RefusedHooks Refusal = "this repository has commit hooks"
	// RefusedJSON is --json, whose contract is that the document always carries
	// a real commit hash and never a promise.
	RefusedJSON Refusal = "--json is synchronous by contract"
	// RefusedAmend is --amend. An amend replaces HEAD rather than extending it,
	// so its parent is HEAD~1 and the atomic ref move this relies on would be
	// comparing against the wrong commit.
	RefusedAmend Refusal = "--amend rewrites the current commit"
	// RefusedNoCampaign is a repository outside any campaign, where there is no
	// queue to enqueue into.
	RefusedNoCampaign Refusal = "not in a campaign"
	// RefusedNoPaths is a bookkeeping commit with no explicit path list.
	RefusedNoPaths Refusal = "no explicit paths to commit"
	// RefusedPreStaged is a bookkeeping commit that depends on content already
	// in the user's index.
	RefusedPreStaged Refusal = "the commit reads already-staged content"
	// RefusedNoWriter is --auto-write with no writer configured.
	//
	// Deferring would turn a configuration mistake into a failed job the user
	// discovers later, in place of the immediate error that tells them exactly
	// what to put in campaign.yaml. Refusing here lets the synchronous path
	// raise that error unchanged: a config error belongs to the command, not
	// to the queue.
	RefusedNoWriter Refusal = "no commit message writer is configured"
)

// Request is everything the decision needs.
type Request struct {
	// CampaignRoot is the campaign the queue lives in. Empty means none.
	CampaignRoot string
	// RepoPath is the absolute path of the repository being committed.
	RepoPath string
	// JSON is true when the caller is emitting a machine document.
	JSON bool
	// Amend is true for --amend.
	Amend bool
}

// Allowed reports whether an --auto-write commit in this repository may defer,
// and why not when it may not.
//
// Ordered cheapest first, but the order is also the order a user would want to
// hear about: their own configuration before camp's internal constraints.
func Allowed(ctx context.Context, req Request) (bool, Refusal) {
	if Disabled() {
		return false, RefusedDisabled
	}
	if req.JSON {
		return false, RefusedJSON
	}
	if req.Amend {
		return false, RefusedAmend
	}
	if req.CampaignRoot == "" || jobs.RepoForPath(req.CampaignRoot, req.RepoPath) == "" {
		return false, RefusedNoCampaign
	}
	if _, err := autowrite.LoadCommitMessageHook(ctx, req.CampaignRoot); err != nil {
		return false, RefusedNoWriter
	}
	// Last because it is the only one that shells out to git.
	if git.HasCommitHooks(ctx, req.RepoPath) {
		return false, RefusedHooks
	}
	return true, ""
}

// Disabled reports whether CAMP_NO_DEFER asks for inline behavior.
//
// Any value other than the empty string and "0" counts, because someone
// exporting CAMP_NO_DEFER=false is asking for it off, not on. Guessing the
// other way would defer for a user who explicitly said not to.
func Disabled() bool {
	v := strings.TrimSpace(os.Getenv(EnvNoDefer))
	return v != "" && v != "0"
}

// AllowedForPaths reports whether camp's own bookkeeping commit may defer.
//
// Narrower than Allowed: a bookkeeping job commits a fixed list of paths
// through a temp index, so it needs neither a writer nor the user's index. What
// it does need is paths, and it must not touch the real one.
func AllowedForPaths(ctx context.Context, campaignRoot, repoPath string, paths, preStaged []string) (bool, Refusal) {
	if Disabled() {
		return false, RefusedDisabled
	}
	if len(paths) == 0 {
		// No explicit paths means the synchronous path would stage everything,
		// and a deferred job may never do that: it runs at an unknown later
		// moment and would sweep in whatever is present then.
		return false, RefusedNoPaths
	}
	if len(preStaged) > 0 {
		// Pre-staged paths are read out of the user's real index, right now.
		// A job running later would commit the worktree content instead, which
		// is a different commit than the one the user asked for.
		return false, RefusedPreStaged
	}
	if campaignRoot == "" || jobs.RepoForPath(campaignRoot, repoPath) == "" {
		return false, RefusedNoCampaign
	}
	if git.HasCommitHooks(ctx, repoPath) {
		return false, RefusedHooks
	}
	return true, ""
}

// EnqueuePaths queues a bookkeeping commit of explicit paths.
//
// The message is composed by the caller and recorded on the job, because
// bookkeeping messages are deterministic. Nothing here consults a writer.
func EnqueuePaths(ctx context.Context, campaignRoot, repoPath, message string, paths []string) (*jobs.Job, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// Content is captured here, not read at execution. A job that only
	// recorded paths would defer the read as well as the write, so an edit made
	// between the enqueue and the run would land under this message.
	//
	// A capture failure degrades to a path-only job rather than costing the
	// user their commit: the coalescing risk is real but smaller than not
	// committing at all, and the failure is a repository camp could not read.
	// Degrading silently is not an option, so the worker log records it; the
	// job itself also shows it, since a captured job carries blobs.
	//
	// A nested repository is the exception, because there the degraded job is
	// wrong rather than merely coarser. See the refusal below.
	blobs, err := git.CaptureBlobs(ctx, repoPath, paths)
	switch {
	case errors.Is(err, git.ErrNestedRepo):
		// A submodule cannot be deferred at all, so this is the one capture
		// failure that refuses the job rather than degrading it. Git records
		// the path as a gitlink pointing at the nested HEAD, which is neither
		// something the enqueuer can snapshot nor something a job may commit
		// from an execution-time read: `camp project add` would publish
		// whatever that submodule's HEAD had become by the time the worker
		// ran. Returning an error puts the caller back on the synchronous
		// path, where `git add` records the pointer the user just created.
		jobs.LogEvent(campaignRoot,
			"capture-refused repo=%s paths=%d err=%v; committing synchronously instead",
			jobs.RepoForPath(campaignRoot, repoPath), len(paths), err)
		return nil, err
	case err != nil:
		jobs.LogEvent(campaignRoot,
			"capture-degraded repo=%s paths=%d err=%v; the job will commit execution-time content",
			jobs.RepoForPath(campaignRoot, repoPath), len(paths), err)
		blobs = nil
	}

	return jobs.Enqueue(ctx, campaignRoot, jobs.Job{
		Kind:    jobs.KindCommitPaths,
		Class:   jobs.ClassCommit,
		Repo:    jobs.RepoForPath(campaignRoot, repoPath),
		Paths:   paths,
		Blobs:   toJobBlobs(blobs),
		Message: message,
	})
}

// toJobBlobs converts captured content into the job document's form.
func toJobBlobs(refs []git.BlobRef) []jobs.BlobRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]jobs.BlobRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, jobs.BlobRef{Path: r.Path, Mode: r.Mode, SHA: r.SHA})
	}
	return out
}

// SpawnWorker starts a worker for a repository's lane if one is needed.
//
// Separate from EnqueuePaths so the job is durable before anything is spawned:
// a worker that started first could find the lane empty, exit, and leave the
// job for whatever command comes next.
func SpawnWorker(ctx context.Context, campaignRoot, repoPath string) {
	jobs.SpawnIfNeeded(ctx, campaignRoot, jobs.RepoForPath(campaignRoot, repoPath))
}

// EnqueueOptions carries what the worker cannot re-derive.
//
// Every field here exists because the enqueuing process knows something a
// detached worker does not: the writer's environment and the campaign tag both
// come from the user's working directory, and the follow-up encodes a decision
// the user's flags made.
type EnqueueOptions struct {
	// WriterEnv is the CAMP_* environment the message writer would have had.
	WriterEnv []string
	// MessagePrefix is the campaign tag to put on the subject line.
	MessagePrefix string
	// Then is a follow-up to enqueue once this commit lands.
	Then *jobs.Follow
}

// Enqueued is what a deferred commit produced.
type Enqueued struct {
	// Job is the queued job, with its allocated identity.
	Job *jobs.Job
	// Tree is the captured tree SHA.
	Tree string
	// Parent is the HEAD the tree was captured against.
	Parent string
}

// Enqueue captures the staged tree and queues a commit against it.
//
// writerEnv carries the CAMP_* variables the message writer would have received
// had this commit run in the foreground. They are recorded on the job because
// the worker cannot re-derive them: the workitem context comes from the
// enqueuing process's working directory, and a detached worker has none.
//
// Called after staging has already happened in the foreground, so the index
// holds exactly what the user asked to commit. The tree is captured here rather
// than in the worker because it is the snapshot: an immutable, content
// addressed object that cannot be affected by anything the user does next.
//
// The parent is recorded for the same reason the tree is. At execution time the
// worker moves HEAD only if it still points at this commit, so a queued commit
// can never silently discard work someone else committed in the gap.
func Enqueue(ctx context.Context, campaignRoot, repoPath string, opts EnqueueOptions) (*Enqueued, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	tree, err := git.WriteTree(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	parent, err := git.FullHash(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	// Refuse empty snapshots. A deferred commit whose tree equals its parent
	// would land a no-op commit (and previously a filler message when the
	// writer correctly reported "no staged changes"). Same sentinel the
	// synchronous path uses when there is nothing to commit.
	parentTree, err := git.TreeSHA(ctx, repoPath, parent)
	if err != nil {
		return nil, err
	}
	if tree == parentTree {
		return nil, git.ErrNoChanges
	}

	repo := jobs.RepoForPath(campaignRoot, repoPath)
	job, err := jobs.Enqueue(ctx, campaignRoot, jobs.Job{
		Kind:          jobs.KindCommitTree,
		Class:         jobs.ClassCommit,
		Repo:          repo,
		Tree:          tree,
		Parent:        parent,
		AutoWrite:     true,
		Env:           opts.WriterEnv,
		MessagePrefix: opts.MessagePrefix,
		Then:          opts.Then,
	})
	if err != nil {
		return nil, err
	}

	// Spawn after the job is durable, never before: a worker that started
	// first could find the lane empty and exit, leaving the job for the next
	// command to discover.
	jobs.SpawnIfNeeded(ctx, campaignRoot, repo)

	return &Enqueued{Job: job, Tree: tree, Parent: parent}, nil
}
