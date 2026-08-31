// Package commitkit provides a stable public API for campaign-aware git
// operations. It wraps camp's internal git and campaign packages so that
// external tools (e.g. fest) can import them without depending on internal
// implementation paths.
//
// All staging and commit operations use automatic lock retry with stale
// lock cleanup, making them resilient to index.lock contention.
//
// SyncSubmoduleRef commits only the requested submodule gitlink path and
// preserves unrelated staged campaign-root content.
package commitkit

import (
	"context"
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// ErrNoChanges is returned when there are no changes to commit.
var ErrNoChanges = git.ErrNoChanges

// JoinMessages assembles repeated -m/--message flag values using git's exact
// multi -m semantics: empty values are dropped and the remaining values are
// joined with a blank line, so each becomes its own paragraph and the first is
// the commit subject. A single non-empty value is returned unchanged (embedded
// newlines preserved). Zero values, or an all-empty input, yield "" so callers
// keep their existing "no message" path (prompt, editor, or error) intact.
//
// This must run before the campaign tag is prepended so the tag lands on the
// real subject line rather than a body paragraph.
func JoinMessages(messages []string) string {
	nonEmpty := make([]string, 0, len(messages))
	for _, m := range messages {
		if m != "" {
			nonEmpty = append(nonEmpty, m)
		}
	}
	return strings.Join(nonEmpty, "\n\n")
}

// CommitOptions configures a commit operation.
type CommitOptions struct {
	Message    string
	Amend      bool
	AllowEmpty bool
	Author     string // Optional: "Name <email>"
}

// FormatCampaignTag returns the legacy id-only "[OBEY-CAMPAIGN-{id}]" prefix.
func FormatCampaignTag(campaignID string) string {
	return git.FormatCampaignTag(campaignID)
}

// PrependCampaignTag prepends the legacy id-only tag to a message.
func PrependCampaignTag(campaignID, message string) string {
	return git.PrependCampaignTag(campaignID, message)
}

// TagComponents is the parsed form of a campaign tag.
type TagComponents = git.TagComponents

// FormatTag is the canonical tag emitter: it composes a tag from any subset of
// TagComponents, and every other Format/Prepend helper here routes through it.
// Use it when you need the segments the positional helpers cannot express.
//
// Those are Phase and Sequence, emitted as "PH-<n>" and "SQ-<n>" after the
// "FE-" festival ref, which is how fest records where inside a festival a
// commit happened. They are dependent segments: Phase is emitted only when
// FestRef is set, and Sequence only when Phase is, since both index into a
// festival and mean nothing without one. Each must also be 1 to 4 digits, the
// shape the parser accepts. Supplying neither yields exactly the tag the
// positional helpers produce today.
//
// FormatTag never fails: anything it cannot emit verbatim is dropped, which
// leaves the caller no signal. Run ValidateTagComponents first to learn what
// would be dropped.
func FormatTag(tc TagComponents) string {
	return git.FormatTag(tc)
}

// ValidateTagComponents reports where FormatTag's output would not faithfully
// carry tc: a missing campaign id, a value failing its segment's shape check,
// or a phase or sequence whose parent segment is missing or itself dropped.
//
// It returns nil when FormatTag's output reparses with zero warnings and
// components equal to tc after the normalization the emitter applies: the
// campaign id truncated to 8 characters, the campaign name slugified into the
// head or dropped for the legacy marker, and the WI- / NT- prefixes added to
// bare refs. Nil does not promise tc is emitted verbatim (a 16-character
// campaign id validates clean and is still truncated), only that nothing is
// lost that ParseTag cannot give back.
//
// A reported problem is one of two kinds, and the message says which:
// FormatTag guards the phase and sequence, so a malformed one is dropped,
// while a malformed quest id, festival ref, workitem ref, or note ref is
// written into the tag as given and degrades only when it is parsed back.
//
// The result is a joined error whose message names every field. Enumerate the
// individual problems by unwrapping it through interface{ Unwrap() []error }.
//
// It is advisory. FormatTag stays lenient, so a caller that does not mind
// losing a segment can skip it; a caller that would rather fix its input than
// write a commit tag missing its festival locator should call this first.
func ValidateTagComponents(tc TagComponents) error {
	return git.ValidateTagComponents(tc)
}

// FormatContextTagsFull composes the legacy id-only tag from any subset of
// (campaign id, quest id, festival ref, workitem ref). Use FormatContextTagsFullNamed
// to emit a name-style "[{name}:{id}]" tag.
func FormatContextTagsFull(campaignID, questID, festRef, workitemRef string) string {
	return git.FormatContextTagsFull("", campaignID, questID, festRef, workitemRef)
}

// FormatContextTagsFullNamed is FormatContextTagsFull with the campaign name as
// the leading token, falling back to the legacy form when campaignName is empty.
func FormatContextTagsFullNamed(campaignName, campaignID, questID, festRef, workitemRef string) string {
	return git.FormatContextTagsFull(campaignName, campaignID, questID, festRef, workitemRef)
}

// PrependContextTagsFull prepends the legacy id-only tag to a message.
func PrependContextTagsFull(campaignID, questID, festRef, workitemRef, message string) string {
	return git.PrependContextTagsFull("", campaignID, questID, festRef, workitemRef, message)
}

// PrependContextTagsFullNamed prepends the name-style tag to a message.
func PrependContextTagsFullNamed(campaignName, campaignID, questID, festRef, workitemRef, message string) string {
	return git.PrependContextTagsFull(campaignName, campaignID, questID, festRef, workitemRef, message)
}

// ParseTag extracts the components of a campaign tag from a commit subject.
// Returns a zero-valued TagComponents when no tag is present.
func ParseTag(subject string) TagComponents {
	return git.ParseTag(subject)
}

// TagParseWarning records a degraded parse from ParseTagDetailed.
// Re-exported from internal/git.
type TagParseWarning = git.TagParseWarning

// ParseTagDetailed is the warnings-aware peer of ParseTag. Callers that
// need to surface "tag was malformed" diagnostics (commit query output,
// doctor, etc.) should call this instead of ParseTag.
func ParseTagDetailed(subject string) (TagComponents, []TagParseWarning) {
	return git.ParseTagDetailed(subject)
}

// GitLogTraversalArgs are the `git log` traversal flags every commit-tag
// reader must share so they observe identical commit history: --all walks
// every ref (not just the default branch), and merge commits are included by
// omitting --no-merges. `camp audit backfill` (internal/audit) and
// `camp workitem commits --source scan` (internal/commands/workitem) both
// build their git-log invocation on top of this shared slice instead of
// duplicating the flags, so neither can drift back to --no-merges or a
// default-branch-only scan without breaking a test that reads this function.
// Diverging here reintroduces the 13-vs-20 ledger/scan SHA gap from camp#615.
func GitLogTraversalArgs() []string {
	return []string{"--all"}
}

// DetectCampaign finds the campaign root by walking up from the current
// working directory. Returns the campaign ID string from the campaign's
// config, or an error if the working directory is not inside a campaign.
func DetectCampaign(ctx context.Context) (string, error) {
	root, err := campaign.DetectFromCwd(ctx)
	if err != nil {
		return "", err
	}

	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return "", camperrors.Wrapf(err, "commitkit: load camp config at %s", root)
	}

	return cfg.ID, nil
}

// LoadCampaignID reads the campaign ID from the campaign.yaml located at
// campaignRoot. campaignRoot must be the directory that contains .campaign/.
func LoadCampaignID(ctx context.Context, campaignRoot string) (string, error) {
	cfg, err := config.LoadCampaignConfig(ctx, campaignRoot)
	if err != nil {
		return "", camperrors.Wrapf(err, "commitkit: load camp config at %s", campaignRoot)
	}

	return cfg.ID, nil
}

// DetectCampaignName is DetectCampaign for the campaign name, for callers that
// build name-style tags (FormatContextTagsFullNamed).
func DetectCampaignName(ctx context.Context) (string, error) {
	root, err := campaign.DetectFromCwd(ctx)
	if err != nil {
		return "", err
	}

	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return "", camperrors.Wrapf(err, "commitkit: load camp config at %s", root)
	}

	return cfg.Name, nil
}

// LoadCampaignName reads the campaign name from the campaign.yaml located at
// campaignRoot. campaignRoot must be the directory that contains .campaign/.
func LoadCampaignName(ctx context.Context, campaignRoot string) (string, error) {
	cfg, err := config.LoadCampaignConfig(ctx, campaignRoot)
	if err != nil {
		return "", camperrors.Wrapf(err, "commitkit: load camp config at %s", campaignRoot)
	}

	return cfg.Name, nil
}

// StageAll stages all changes in the repository at repoPath.
// Uses automatic lock retry with stale lock cleanup.
//
// It takes no limits by design. The staging guard runs inside the staging
// chokepoint and resolves its own thresholds from repoPath, so a caller
// inherits large-file and bulk protection here without passing anything and
// without a code change. Use CheckStaging only to preflight.
//
// It discards what the guard did. When the guard excludes an over-threshold
// file the stage still succeeds, so a consumer using this form commits without
// the file and has nothing to tell the user about it. Use StageAllWithOutcome
// when the consumer renders its own output; a refusal still surfaces here as a
// *GuardBlockedError.
func StageAll(ctx context.Context, repoPath string) error {
	_, err := StageAllWithOutcome(ctx, repoPath)
	return err
}

// StageAllWithOutcome is StageAll, returning what the guard decided.
//
// The outcome is nil when the guard found nothing worth reporting, which is the
// ordinary case. A non-nil outcome means the consumer has something to say:
// files excluded, tracked files flagged, or the guard unable to run at all.
func StageAllWithOutcome(ctx context.Context, repoPath string) (*StageOutcome, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	drainQuietly(ctx, repoPath)
	return git.StageWithGuard(ctx, repoPath, nil)
}

// StageAllWithOptions is StageAllWithOutcome with per-invocation overrides of
// the guard's decision. It exists for the consumer flag that means "commit it
// anyway": fest's --commit-large forwards here exactly as camp's own
// --commit-large reaches the guard, so the override behaves identically in
// both tools.
func StageAllWithOptions(ctx context.Context, repoPath string, opts StageOptions) (*StageOutcome, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	drainQuietly(ctx, repoPath)
	return git.StageWithGuardOptions(ctx, repoPath, nil, opts)
}

// StageFiles stages specific files in the repository at repoPath.
// Uses automatic lock retry with stale lock cleanup.
//
// Like StageAll, this discards the guard's outcome; use StageFilesWithOutcome
// when the consumer renders its own output.
func StageFiles(ctx context.Context, repoPath string, files ...string) error {
	_, err := StageFilesWithOutcome(ctx, repoPath, files...)
	return err
}

// StageFilesWithOutcome is StageFiles, returning what the guard decided.
func StageFilesWithOutcome(ctx context.Context, repoPath string, files ...string) (*StageOutcome, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// An empty list stays an error rather than becoming stage-everything. The
	// guard path treats no files as "stage the whole tree", which is the
	// opposite of what a caller passing an empty slice meant.
	if len(files) == 0 {
		return nil, git.ErrNoFilesSpecified
	}
	drainQuietly(ctx, repoPath)
	return git.StageWithGuard(ctx, repoPath, files)
}

// StageFilesWithOptions is StageFilesWithOutcome with per-invocation overrides
// of the guard's decision. It exists for the consumer flag that means "commit
// it anyway": fest's --commit-large forwards here exactly as camp's own
// --commit-large reaches the guard, so the override behaves identically in
// both tools on the file-list path, not just the stage-all path.
func StageFilesWithOptions(ctx context.Context, repoPath string, opts StageOptions, files ...string) (*StageOutcome, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// Same reasoning as StageFilesWithOutcome: an empty list stays an error
	// rather than silently becoming stage-everything.
	if len(files) == 0 {
		return nil, git.ErrNoFilesSpecified
	}
	drainQuietly(ctx, repoPath)
	return git.StageWithGuardOptions(ctx, repoPath, files, opts)
}

// HasStagedChanges reports whether there are staged changes ready to commit
// in the repository at repoPath.
func HasStagedChanges(ctx context.Context, repoPath string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return git.HasStagedChanges(ctx, repoPath)
}

// Commit creates a git commit in the repository at repoPath.
// Uses automatic lock retry with stale lock cleanup.
// Returns ErrNoChanges if there is nothing to commit.
func Commit(ctx context.Context, repoPath string, opts CommitOptions) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	drainQuietly(ctx, repoPath)
	return git.Commit(ctx, repoPath, &git.CommitOptions{
		Message:    opts.Message,
		Amend:      opts.Amend,
		AllowEmpty: opts.AllowEmpty,
		Author:     opts.Author,
	})
}

// CommitAll stages all changes and commits them with the given message.
// Returns ErrNoChanges if there is nothing to commit.
// Uses automatic lock retry with stale lock cleanup.
func CommitAll(ctx context.Context, repoPath, message string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Drains like the other history-moving entrypoints. Missing it here made
	// the guarantee depend on which function a consumer happened to call,
	// which is exactly what wiring the drain into all of them was meant to
	// stop.
	drainQuietly(ctx, repoPath)
	return git.CommitAll(ctx, repoPath, message)
}

// ShortHash returns the short commit hash of HEAD in the repository at repoPath.
func ShortHash(ctx context.Context, repoPath string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	hash, err := git.Output(ctx, repoPath, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", camperrors.Wrap(err, "commitkit: rev-parse --short HEAD")
	}
	return hash, nil
}

// SyncSubmoduleRef stages the updated submodule pointer for projectRelPath
// inside the campaign root and commits it with a campaign-tagged message.
// projectRelPath is the path to the submodule relative to campaignRoot
// (e.g. "projects/fest").
//
// It is a no-op and returns nil when the submodule pointer has not changed
// for projectRelPath.
//
// Uses automatic lock retry with stale lock cleanup.
func SyncSubmoduleRef(ctx context.Context, campaignRoot, projectRelPath, campaignID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Stage only the submodule ref — not the entire working tree.
	if err := git.StageFiles(ctx, campaignRoot, projectRelPath); err != nil {
		return camperrors.Wrapf(err, "commitkit: stage submodule %s", projectRelPath)
	}

	// Check only the ref path so unrelated staged root content is preserved.
	hasChanges, err := git.HasStagedPathChange(ctx, campaignRoot, projectRelPath)
	if err != nil {
		return camperrors.Wrapf(err, "commitkit: check staged submodule %s", projectRelPath)
	}
	if !hasChanges {
		return nil // No-op: submodule pointer hasn't changed.
	}

	msg := git.PrependCampaignTag(campaignID,
		fmt.Sprintf("sync submodule ref: %s", projectRelPath))

	if err := git.CommitScoped(ctx, campaignRoot, []string{projectRelPath}, &git.CommitOptions{Message: msg}); err != nil {
		return camperrors.Wrapf(err, "commitkit: commit submodule ref for %s", projectRelPath)
	}

	return nil
}
