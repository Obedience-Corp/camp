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
	"strconv"
	"strings"
	"time"

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
// a formatted error string. The deferred-commit worker needs exactly that: a
// failing writer parks the job instead of committing with an invented
// subject, and the parked job's LastError needs the writer's own diagnostic,
// not a message built by scraping err.Error(), which would carry camp's
// wrapping and the writer's ANSI codes into that record.
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

// DefaultWriterTimeout bounds one deferred run of the writer.
//
// Must stay strictly above ob commit's shipped --queue-timeout (10m) or camp
// kills a writer still allowed to wait for a slot. jobs.drainMaxWait uses
// this same value so a drain cannot give up first.
const DefaultWriterTimeout = 12 * time.Minute

// writerWaitDelay bounds the wait between a killed writer exiting and its
// output pipes closing.
//
// Killing a writer does not close a pipe that something it started still
// holds, and os/exec waits for the pipes as well as for the process. Without
// this a writer whose grandchild outlived it hangs the worker on Wait forever,
// which is the exact failure the timeout exists to bound: the bound has to
// cover the wait too, or it is not a bound.
const writerWaitDelay = 2 * time.Second

// CommitMessageHook is the configured commit message writer command.
type CommitMessageHook struct {
	// Command is executed as-written from the target repository.
	Command string
	// Timeout bounds one deferred run. Never zero: an unset config takes
	// DefaultWriterTimeout, so a caller that honors this field cannot end up
	// running the writer unbounded by forgetting to check for zero.
	Timeout time.Duration
}

// LoadCommitMessageHook loads hooks.commit_message from campaign config.
func LoadCommitMessageHook(ctx context.Context, campaignRoot string) (*CommitMessageHook, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cfg, err := config.LoadCampaignConfig(ctx, campaignRoot)
	if err != nil {
		return nil, camperrors.Wrapf(err, "commitkit: load camp config at %s", campaignRoot)
	}

	command := strings.TrimSpace(cfg.Hooks.CommitMessage.Command)
	if command == "" {
		return nil, ErrCommitMessageHookNotConfigured
	}

	timeout, err := parseWriterTimeout(cfg.Hooks.CommitMessage.Timeout)
	if err != nil {
		return nil, err
	}

	return &CommitMessageHook{Command: command, Timeout: timeout}, nil
}

// parseWriterTimeout resolves the configured bound, defaulting when unset.
//
// A value camp cannot parse is an error rather than a silent fallback, the
// same call the staging guards make about their thresholds: a typo must not
// quietly restore the unbounded writer this field exists to bound.
func parseWriterTimeout(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultWriterTimeout, nil
	}
	d, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, camperrors.NewValidation("hooks.commit_message.timeout",
			"must be a duration such as \"5m\", got "+strconv.Quote(raw), err)
	}
	if d <= 0 {
		return 0, camperrors.NewValidation("hooks.commit_message.timeout",
			"must be positive, got "+strconv.Quote(raw)+
				"; there is no way to spell \"let the writer run forever\"", nil)
	}
	return d, nil
}

// WriterTimeout reports the bound a deferred writer runs under, for the
// commands that describe the queue rather than serve it.
//
// Best effort on purpose: a campaign whose config cannot be read still has a
// queue to report on, and a listing that failed because of an unrelated config
// problem would hide the very jobs the user came to look at. The run path uses
// LoadCommitMessageHook, which does refuse a malformed value.
func WriterTimeout(ctx context.Context, campaignRoot string) time.Duration {
	cfg, err := config.LoadCampaignConfig(ctx, campaignRoot)
	if err != nil {
		return DefaultWriterTimeout
	}
	timeout, err := parseWriterTimeout(cfg.Hooks.CommitMessage.Timeout)
	if err != nil {
		return DefaultWriterTimeout
	}
	return timeout
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

// RunOptions configures one run of the writer.
type RunOptions struct {
	// Env are extra KEY=VALUE entries appended to os.Environ().
	Env []string
	// Timeout bounds the run. Zero means unbounded, which is what the
	// foreground path wants: a user watching "generating..." can decide for
	// themselves how long to wait and press Ctrl+C.
	Timeout time.Duration
	// OwnProcessGroup starts the writer in a process group of its own, so
	// cancelling the run kills what the writer started and not merely the
	// shell in front of it.
	//
	// Only the detached worker sets it. In the foreground the writer must stay
	// in the terminal's process group or the user's Ctrl+C stops reaching it,
	// and camp would answer an interrupt by orphaning the very process the
	// user was trying to stop.
	OwnProcessGroup bool
	// DiagnosticOut receives the writer's stderr live. Nil keeps it buffered
	// for the error and prints nothing.
	DiagnosticOut io.Writer
}

// TimeoutError is a writer that was still running when its bound expired.
//
// Typed rather than formatted so the queue can name the timeout and the
// command in the reason it parks with, without either side parsing prose.
type TimeoutError struct {
	// Command is the configured writer, as written in campaign.yaml.
	Command string
	// Timeout is the bound it exceeded.
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return "the commit message writer (" + e.Command + ") did not finish within " +
		e.Timeout.String()
}

// RunCommitMessageCommand executes command exactly as configured from repoPath
// and returns trimmed stdout as the raw commit message.
func RunCommitMessageCommand(ctx context.Context, repoPath, command string) (string, error) {
	return RunCommitMessageCommandWithEnv(ctx, repoPath, command, nil)
}

// RunCommitMessageCommandWithEnv is RunCommitMessageCommand with extra
// environment variables passed to the subprocess (appended to os.Environ()).
func RunCommitMessageCommandWithEnv(ctx context.Context, repoPath, command string, extraEnv []string) (string, error) {
	return RunCommitMessageCommandWithOptions(ctx, repoPath, command,
		RunOptions{Env: extraEnv, DiagnosticOut: os.Stderr})
}

// RunCommitMessageCommandWithOptions is RunCommitMessageCommand with the run
// bounded, isolated, and reported as the caller asks.
func RunCommitMessageCommandWithOptions(
	ctx context.Context, repoPath, command string, opts RunOptions,
) (string, error) {
	return runCommitMessageCommand(ctx, repoPath, command, opts)
}

func runCommitMessageCommandWithEnv(
	ctx context.Context,
	repoPath string,
	command string,
	extraEnv []string,
	diagnosticOut io.Writer,
) (string, error) {
	return runCommitMessageCommand(ctx, repoPath, command,
		RunOptions{Env: extraEnv, DiagnosticOut: diagnosticOut})
}

func runCommitMessageCommand(
	ctx context.Context,
	repoPath string,
	command string,
	opts RunOptions,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	command = strings.TrimSpace(command)
	if command == "" {
		return "", ErrCommitMessageHookNotConfigured
	}

	// The deadline gets a context of its own so a writer that ran out of time
	// stays distinguishable from a camp that was told to stop. The worker
	// needs exactly that difference: a timed-out writer is a verdict on the
	// job and parks it, while a shutdown is a verdict on nothing and puts it
	// back.
	runCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	name, args := shellCommand(command)
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = repoPath
	if len(opts.Env) > 0 {
		cmd.Env = append(os.Environ(), opts.Env...)
	}
	if opts.OwnProcessGroup {
		// The configured command runs through a login shell, which may or may
		// not exec the tool it was given. Killing the shell alone therefore
		// leaves the tool running often enough to matter, and an orphaned
		// `ob commit` holding an LLM session is precisely what the timeout was
		// added to end.
		startInOwnProcessGroup(cmd)
		cmd.Cancel = func() error { return killProcessGroup(cmd.Process.Pid) }
	}
	if opts.Timeout > 0 {
		cmd.WaitDelay = writerWaitDelay
	}
	diagnosticOut := opts.DiagnosticOut

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
		// The caller's own cancellation is reported first: when camp is
		// shutting down, both contexts are done, and the shutdown is the
		// truthful answer.
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if runCtx.Err() != nil {
			return "", &TimeoutError{Command: command, Timeout: opts.Timeout}
		}
		// The writer finished and said its piece, but something it started
		// still holds the output pipe. The message is complete, so take it:
		// failing a job over a stray background process would discard a
		// commit message that was successfully written.
		if message := strings.TrimSpace(stdout.String()); message != "" &&
			errors.Is(err, exec.ErrWaitDelay) {
			return message, nil
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
