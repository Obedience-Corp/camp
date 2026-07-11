package commit

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git"
)

// Result contains the outcome of a commit attempt.
type Result struct {
	Committed bool   // True if commit succeeded
	NoChanges bool   // True if there was nothing to commit
	Err       error  // Set when a commit attempt failed
	Message   string // User-facing message

	// Skipped is true when the commit was never attempted because of a
	// caller-detectable precondition (missing campaign context, or a
	// selective commit that resolved to zero files to stage) rather than
	// git genuinely finding nothing changed. SkipReason explains why.
	// Both preconditions would otherwise look identical to a legitimate
	// "nothing to commit" outcome, so callers that want to surface a
	// symptom like "my capture didn't commit" should check Skipped via
	// WarnIfSkipped instead of relying on Message alone.
	Skipped    bool
	SkipReason string
}

// WarnIfSkipped writes a warning line to w when res reports a guaranteed
// commit skip (see Result.Skipped). It is a no-op otherwise. Committed,
// NoChanges, Message, and Err are unaffected by a skip, so existing
// Message-based output at call sites is unchanged; this only adds an
// explicit, greppable signal for the skip causes that would otherwise be
// silent or indistinguishable from a normal no-op commit.
func WarnIfSkipped(w io.Writer, res Result) {
	if !res.Skipped {
		return
	}
	_, _ = fmt.Fprintf(w, "warning: %s\n", res.SkipReason)
}

// Options configures common commit parameters.
type Options struct {
	CampaignRoot  string   // Path to campaign root
	CampaignID    string   // Campaign ID (truncated to 8 chars)
	CampaignName  string   // Optional campaign name; when empty it is resolved from CampaignRoot
	QuestID       string   // Optional quest ID for additive commit context
	FestivalRef   string   // Optional festival ref for additive commit context
	WorkitemRef   string   // Optional workitem ref (WI-<6 hex>) for additive commit context
	Files         []string // If set, stage only these paths instead of everything
	PreStaged     []string // Paths already staged; copied from the real index into the temp-index commit scope
	SelectiveOnly bool     // When true, never fall back to CommitAll; no-op if Files is empty
}

// resolveCampaignName prefers opts.CampaignName, else loads it from the config
// at opts.CampaignRoot. A failed load yields "" (formatter falls back to legacy).
func resolveCampaignName(ctx context.Context, opts Options) string {
	if opts.CampaignName != "" {
		return opts.CampaignName
	}
	cfg, err := config.LoadCampaignConfig(ctx, opts.CampaignRoot)
	if err != nil {
		return ""
	}
	return cfg.Name
}

// doCommit stages all changes and commits with standardized format.
// Commit failures are non-fatal and reported via Result.
//
// Commit message format:
//
//	[{campaign-name}:{id}] Action: Subject
//
//	Optional description body
func doCommit(ctx context.Context, opts Options, action, subject, description string) Result {
	if opts.CampaignRoot == "" || opts.CampaignID == "" {
		return Result{
			Committed:  false,
			Skipped:    true,
			SkipReason: fmt.Sprintf("%s: missing campaign context (CampaignRoot or CampaignID is empty)", action),
		}
	}

	if opts.SelectiveOnly && len(opts.Files) == 0 && len(opts.PreStaged) == 0 {
		return Result{
			Committed:  false,
			NoChanges:  true,
			Skipped:    true,
			SkipReason: fmt.Sprintf("%s: selective commit requested but no files resolved to stage", action),
			Message:    "(no changes to commit)",
		}
	}

	commitMsg := fmt.Sprintf("%s %s: %s",
		git.FormatContextTagsFull(resolveCampaignName(ctx, opts), opts.CampaignID, opts.QuestID, opts.FestivalRef, opts.WorkitemRef),
		action,
		subject,
	)
	if description != "" {
		commitMsg += "\n\n" + description
	}

	if err := stageAndCommit(ctx, opts, commitMsg); err != nil {
		if errors.Is(err, git.ErrNoChanges) {
			return Result{
				Committed: false,
				NoChanges: true,
				Message:   "(no changes to commit)",
			}
		}
		return Result{
			Committed: false,
			Err:       err,
			Message:   fmt.Sprintf("git commit failed: %v", err),
		}
	}

	return Result{
		Committed: true,
		Message:   "Committed changes to git",
	}
}

// stageAndCommit snapshots scoped commits through a temporary git index.
// opts.Files are added to a temp index at add time; opts.PreStaged-only commits
// copy the real index so the commit captures exactly the staged blobs.
// When SelectiveOnly is true and no paths exist, returns ErrNoChanges instead
// of falling back to CommitAll. Otherwise all changes are staged (legacy behavior).
func stageAndCommit(ctx context.Context, opts Options, message string) error {
	commitScope := append(append([]string{}, opts.Files...), opts.PreStaged...)
	if len(commitScope) == 0 {
		if opts.SelectiveOnly {
			return git.ErrNoChanges
		}
		return git.CommitAll(ctx, opts.CampaignRoot, message)
	}

	tmpPath, realIndex, err := git.BuildTempIndexPath(opts.CampaignRoot)
	if err != nil {
		return err
	}
	defer git.RemoveTempIndex(tmpPath)

	if len(opts.Files) > 0 {
		if err := git.ReadTreeIntoTempIndex(ctx, opts.CampaignRoot, tmpPath); err != nil {
			return err
		}
		if err := git.AddPathsToTempIndex(ctx, opts.CampaignRoot, tmpPath, opts.Files); err != nil {
			return err
		}
		if err := git.ApplyCachedDiffToTempIndex(ctx, opts.CampaignRoot, tmpPath, opts.PreStaged); err != nil {
			return err
		}
	} else if err := git.CopyFile(realIndex, tmpPath); err != nil {
		return err
	}

	expandedScope, err := git.ExpandTrackedPathsFromTempIndex(ctx, opts.CampaignRoot, tmpPath, commitScope)
	if err != nil {
		return err
	}
	if len(expandedScope) == 0 {
		return git.ErrNoChanges
	}

	if err := git.Commit(ctx, opts.CampaignRoot, &git.CommitOptions{
		Message:       message,
		TempIndexPath: tmpPath,
	}); err != nil {
		return err
	}
	return git.ResetIndexToHead(ctx, opts.CampaignRoot, expandedScope)
}
