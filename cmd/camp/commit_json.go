package main

import (
	"context"
	"encoding/json"
	"io"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// CommitJSONVersion is the schema version of `camp commit --json`.
const CommitJSONVersion = "commit/v1alpha1"

// commitJSONResult is the document `camp commit --json` writes to stdout.
//
// The load-bearing field is Excluded. A machine caller that only sees a commit
// hash cannot tell that a file it wrote never entered the commit; without this
// array, camp's automatic handling would be invisible to exactly the callers
// least able to notice it from prose.
//
// The contract is synchronous: this document always carries a real commit
// hash, never a job id or a promise. Deferral, added later, must not change
// that, and the test asserting it exists now so the guarantee cannot regress
// silently.
type commitJSONResult struct {
	SchemaVersion string `json:"schema_version"`
	// OK is true when the commit landed.
	OK bool `json:"ok"`
	// Repo is the repository the commit was made in.
	Repo string `json:"repo"`
	// Commit is the full commit hash. Never empty when OK is true.
	Commit string `json:"commit"`
	// Staged counts the paths that entered the commit.
	Staged int `json:"staged"`
	// Excluded lists files the guard kept out. Always an array, never null.
	Excluded []excludedFileJSON `json:"excluded"`
	// ArtifactRootsDeclared lists roots camp declared during this commit.
	// Always an array, never null.
	ArtifactRootsDeclared []string `json:"artifact_roots_declared"`
}

// excludedFileJSON is one file the guard kept out of the commit.
type excludedFileJSON struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	// Reason is why the file was excluded. The complete set:
	//
	//   size_guard           over the threshold; camp declared an artifact
	//                        root for it, named in ArtifactRoot
	//   needs_root_decision  over the threshold, but camp would not choose a
	//                        root (campaign root, .campaign/, a repo root)
	//   project_large_file   over the threshold inside a project, where an
	//                        artifact root would keep it off the remote
	Reason string `json:"reason"`
	// ArtifactRoot is the root camp declared, set only for size_guard.
	ArtifactRoot string `json:"artifact_root,omitempty"`
}

// Exclusion reasons. size_guard is the auto-handled case; the other two come
// from the refusal paths and are already named in GuardHandling.
const reasonSizeGuard = "size_guard"

// newCommitJSONResult builds the document with every slice initialized.
//
// Nil slices marshal to null, which has broken camp --json consumers before:
// a caller iterating `.excluded[]` gets a type error rather than zero
// iterations. The empty-slice initialization is the contract, not a detail.
func newCommitJSONResult(repo string) *commitJSONResult {
	return &commitJSONResult{
		SchemaVersion:         CommitJSONVersion,
		Repo:                  repo,
		Excluded:              []excludedFileJSON{},
		ArtifactRootsDeclared: []string{},
	}
}

// applyGuardHandling folds what the guard did into the result.
func (r *commitJSONResult) applyGuardHandling(h *cmdutil.GuardHandling) {
	if h == nil {
		return
	}
	for _, d := range h.Declared {
		r.ArtifactRootsDeclared = append(r.ArtifactRootsDeclared, d.Root)
		for _, v := range d.Files {
			r.Excluded = append(r.Excluded, excludedFileJSON{
				Path: v.Path, Size: v.Size, Reason: reasonSizeGuard, ArtifactRoot: d.Root,
			})
		}
	}
	for _, f := range h.Refused {
		r.Excluded = append(r.Excluded, excludedFileJSON{
			Path: f.Violation.Path, Size: f.Violation.Size, Reason: f.Reason,
		})
	}
	for _, f := range h.ProjectExcluded {
		r.Excluded = append(r.Excluded, excludedFileJSON{
			Path: f.Violation.Path, Size: f.Violation.Size, Reason: f.Reason,
		})
	}
}

// finalize records the landed commit, resolving the full hash.
func (r *commitJSONResult) finalize(ctx context.Context, repoPath string) error {
	hash, err := git.FullHash(ctx, repoPath)
	if err != nil {
		return camperrors.Wrapf(err, "resolve commit hash in %s", repoPath)
	}
	staged, err := git.CommitPathCount(ctx, repoPath)
	if err != nil {
		return camperrors.Wrapf(err, "count committed paths in %s", repoPath)
	}
	r.OK = true
	r.Commit = hash
	r.Staged = staged
	return nil
}

// emit writes the document as the only thing on stdout.
func (r *commitJSONResult) emit(out io.Writer) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(r)
}
