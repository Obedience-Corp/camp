// Package autowrite runs the configured commit message writer.
//
// It lives in internal/ rather than pkg/commitkit because two callers need it
// and one of them cannot reach commitkit: the deferred-commit worker in
// internal/jobs generates a message for a queued --auto-write commit, and
// commitkit already imports internal/jobs for DrainJobs. Extracting the
// implementation here is what breaks that cycle; pkg/commitkit re-exports it
// unchanged, so the public API is exactly what it was.
package autowrite

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ErrCommitMessageHookNotConfigured is returned when --auto-write is requested
// but .campaign/campaign.yaml does not configure hooks.commit_message.command.
var ErrCommitMessageHookNotConfigured = errors.New(`auto-write commit message command is not configured

Configure a command in .campaign/campaign.yaml. It is run from the target
repository's working tree and its stdout is used verbatim as the commit
message, so any tool that emits a message on stdout works.

hooks:
  commit_message:
    command: <your-commit-message-tool>

Example (using the obey CLI):

hooks:
  commit_message:
    command: ob commit --print-session-id`)

// ErrCommitMessageHookEmptyOutput is returned when the hook succeeds but writes
// no commit message to stdout.
var ErrCommitMessageHookEmptyOutput = errors.New("auto-write commit message command produced no message")

// WriterError is a commit message writer that ran and did not produce a
// message.
//
// It exists so callers can act on the writer's own diagnostic without parsing
// a formatted error string. The deferred-commit worker needs exactly that: it
// commits anyway when the writer fails and puts the reason in the commit
// message, and a message built by scraping err.Error() would carry camp's
// wrapping and the writer's ANSI codes into git history.
type WriterError struct {
	// Command is the configured writer, as written in campaign.yaml.
	Command string
	// Reason is the writer's own last diagnostic line: sanitized, single
	// line, bounded. Empty when the writer said nothing.
	Reason string
	// Err is the underlying failure.
	Err error
}

func (e *WriterError) Error() string {
	if e.Reason == "" {
		return "auto-write commit message command failed"
	}
	return "auto-write commit message command failed: " + e.Reason
}

func (e *WriterError) Unwrap() error { return e.Err }

// maxWriterReasonBytes bounds the writer diagnostic camp repeats.
//
// A writer that fails by printing its own usage emits a screenful, and every
// byte of it would otherwise land twice in the worker log and, for a degraded
// commit, permanently in git history. The operative line is the last one.
const maxWriterReasonBytes = 300

// ansiEscape matches the CSI sequences a writer emits when it renders help
// text to a pipe. Broader than a fixed list of color constants on purpose: the
// point is that nothing an arbitrary user-configured tool prints can reach a
// commit message as an escape code.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// writerReason reduces a writer's stderr to the one line worth repeating.
//
// The last non-empty line, because tools print their diagnosis after their
// usage, not before it. Sanitized and truncated because this string is
// embedded in text camp writes on the user's behalf.
func writerReason(stderr string) string {
	cleaned := strings.TrimSpace(ansiEscape.ReplaceAllString(stderr, ""))
	if cleaned == "" {
		return ""
	}
	lines := strings.Split(cleaned, "\n")
	reason := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if trimmed := strings.TrimSpace(lines[i]); trimmed != "" {
			reason = trimmed
			break
		}
	}
	if len(reason) > maxWriterReasonBytes {
		reason = reason[:maxWriterReasonBytes] + "..."
	}
	return reason
}

// commitAmendEnv is the Camp-to-writer amend signal.
const commitAmendEnv = "CAMP_COMMIT_AMEND=1"

// WithCommitAmendEnv adds the amend signal when amend is true. Writers use this
// explicit contract instead of inferring amend mode from an empty staged index.
func WithCommitAmendEnv(env []string, amend bool) []string {
	if !amend {
		return env
	}
	return append(env, commitAmendEnv)
}

// CommitMessageHook is the configured commit message writer command.
type CommitMessageHook struct {
	Command string
}

// LoadCommitMessageHook loads hooks.commit_message.command from campaign config.
func LoadCommitMessageHook(ctx context.Context, campaignRoot string) (*CommitMessageHook, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cfg, err := config.LoadCampaignConfig(ctx, campaignRoot)
	if err != nil {
		return nil, camperrors.Wrapf(err, "commitkit: load campaign config at %s", campaignRoot)
	}

	command := strings.TrimSpace(cfg.Hooks.CommitMessage.Command)
	if command == "" {
		return nil, ErrCommitMessageHookNotConfigured
	}

	return &CommitMessageHook{Command: command}, nil
}

// AutoWriteCommitMessage runs the configured commit message hook from repoPath.
func AutoWriteCommitMessage(ctx context.Context, campaignRoot, repoPath string) (string, error) {
	return AutoWriteCommitMessageWithEnv(ctx, campaignRoot, repoPath, nil)
}

// AutoWriteCommitMessageWithEnv is AutoWriteCommitMessage with extra
// environment variables passed to the hook subprocess. extraEnv entries are
// appended to os.Environ() — they take precedence on duplicate keys.
//
// Use commitkit.WorkitemEnv (and any future *Env helpers) to build the
// extraEnv slice so the variable contract stays in one place.
func AutoWriteCommitMessageWithEnv(ctx context.Context, campaignRoot, repoPath string, extraEnv []string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	hook, err := LoadCommitMessageHook(ctx, campaignRoot)
	if err != nil {
		return "", err
	}

	return RunCommitMessageCommandWithEnv(ctx, repoPath, hook.Command, extraEnv)
}

// RunCommitMessageCommand executes command exactly as configured from repoPath
// and returns trimmed stdout as the raw commit message.
func RunCommitMessageCommand(ctx context.Context, repoPath, command string) (string, error) {
	return RunCommitMessageCommandWithEnv(ctx, repoPath, command, nil)
}

// RunCommitMessageCommandWithEnv is RunCommitMessageCommand with extra
// environment variables passed to the subprocess (appended to os.Environ()).
func RunCommitMessageCommandWithEnv(ctx context.Context, repoPath, command string, extraEnv []string) (string, error) {
	return runCommitMessageCommandWithEnv(ctx, repoPath, command, extraEnv, os.Stderr)
}

func runCommitMessageCommandWithEnv(
	ctx context.Context,
	repoPath string,
	command string,
	extraEnv []string,
	diagnosticOut io.Writer,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	command = strings.TrimSpace(command)
	if command == "" {
		return "", ErrCommitMessageHookNotConfigured
	}

	name, args := shellCommand(command)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = repoPath
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	// Tee stderr: keep a copy for error wrapping and forward live diagnostics
	// (progress like "ob: connecting..." / "ob: generating...") to the operator
	// while the hook runs. Tools such as `ob commit --print-session-id` may also
	// emit session_id= on stderr when the hook finishes; that is post-completion
	// diagnostics, not a mid-run recovery handle (a hung generation never
	// finalizes, so no session_id appears). Operators may see stderr twice on
	// failure (live stream + wrapped error); that is intentional for now.
	if diagnosticOut == nil {
		cmd.Stderr = &stderr
	} else {
		cmd.Stderr = io.MultiWriter(&stderr, diagnosticOut)
	}

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// Only the reason reaches the error. The full stderr was already
		// streamed live to diagnosticOut, so repeating it here would render a
		// failing writer's help text twice in the same log with nothing to
		// distinguish the copies.
		return "", &WriterError{
			Command: command,
			Reason:  writerReason(stderr.String()),
			Err:     err,
		}
	}

	message := strings.TrimSpace(stdout.String())
	if message == "" {
		return "", &WriterError{
			Command: command,
			Reason:  "the writer exited cleanly without printing a message",
			Err:     ErrCommitMessageHookEmptyOutput,
		}
	}

	return message, nil
}

func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		if comspec := os.Getenv("ComSpec"); comspec != "" {
			return comspec, []string{"/C", command}
		}
		return "cmd", []string{"/C", command}
	}

	if shell := os.Getenv("SHELL"); shell != "" {
		return shell, []string{"-lc", command}
	}
	return "/bin/sh", []string{"-lc", command}
}
