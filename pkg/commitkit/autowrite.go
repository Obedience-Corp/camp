package commitkit

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/autowrite"
)

// The commit message writer's public surface, re-exported for consumers
// outside this module.
//
// The implementation moved to internal/autowrite so the deferred-commit worker
// can call it too: internal/jobs generates a message for a queued
// --auto-write commit, and this package already imports internal/jobs for
// DrainJobs, so a direct dependency would be a cycle. Nothing about the API
// changed; every name below means exactly what it did before.

// ErrCommitMessageHookNotConfigured is returned when --auto-write is requested
// but .campaign/campaign.yaml does not configure hooks.commit_message.command.
var ErrCommitMessageHookNotConfigured = autowrite.ErrCommitMessageHookNotConfigured

// ErrCommitMessageHookEmptyOutput is returned when the hook succeeds but writes
// no commit message to stdout.
var ErrCommitMessageHookEmptyOutput = autowrite.ErrCommitMessageHookEmptyOutput

// CommitMessageHook is the configured commit message writer command.
type CommitMessageHook = autowrite.CommitMessageHook

// LoadCommitMessageHook loads hooks.commit_message.command from campaign config.
func LoadCommitMessageHook(ctx context.Context, campaignRoot string) (*CommitMessageHook, error) {
	return autowrite.LoadCommitMessageHook(ctx, campaignRoot)
}

// AutoWriteCommitMessage runs the configured commit message hook from repoPath.
func AutoWriteCommitMessage(ctx context.Context, campaignRoot, repoPath string) (string, error) {
	return autowrite.AutoWriteCommitMessage(ctx, campaignRoot, repoPath)
}

// AutoWriteCommitMessageWithEnv is AutoWriteCommitMessage with extra
// environment variables passed to the hook subprocess. extraEnv entries are
// appended to os.Environ() — they take precedence on duplicate keys.
//
// Use commitkit.WorkitemEnv (and any future *Env helpers) to build the
// extraEnv slice so the variable contract stays in one place.
func AutoWriteCommitMessageWithEnv(ctx context.Context, campaignRoot, repoPath string, extraEnv []string) (string, error) {
	return autowrite.AutoWriteCommitMessageWithEnv(ctx, campaignRoot, repoPath, extraEnv)
}

// RunCommitMessageCommand executes command exactly as configured from repoPath
// and returns trimmed stdout as the raw commit message.
func RunCommitMessageCommand(ctx context.Context, repoPath, command string) (string, error) {
	return autowrite.RunCommitMessageCommand(ctx, repoPath, command)
}

// RunCommitMessageCommandWithEnv is RunCommitMessageCommand with extra
// environment variables passed to the subprocess (appended to os.Environ()).
func RunCommitMessageCommandWithEnv(ctx context.Context, repoPath, command string, extraEnv []string) (string, error) {
	return autowrite.RunCommitMessageCommandWithEnv(ctx, repoPath, command, extraEnv)
}
