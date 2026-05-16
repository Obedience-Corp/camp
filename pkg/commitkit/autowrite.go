package commitkit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
)

// ErrCommitMessageHookNotConfigured is returned when --auto-write is requested
// but .campaign/campaign.yaml does not configure hooks.commit_message.command.
var ErrCommitMessageHookNotConfigured = errors.New(`auto-write commit message command is not configured

Add this to .campaign/campaign.yaml:
hooks:
  commit_message:
    command: ob commit`)

// ErrCommitMessageHookEmptyOutput is returned when the hook succeeds but writes
// no commit message to stdout.
var ErrCommitMessageHookEmptyOutput = errors.New("auto-write commit message command produced no message")

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
		return nil, fmt.Errorf("commitkit: load campaign config at %s: %w", campaignRoot, err)
	}

	command := strings.TrimSpace(cfg.Hooks.CommitMessage.Command)
	if command == "" {
		return nil, ErrCommitMessageHookNotConfigured
	}

	return &CommitMessageHook{Command: command}, nil
}

// AutoWriteCommitMessage runs the configured commit message hook from repoPath.
func AutoWriteCommitMessage(ctx context.Context, campaignRoot, repoPath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	hook, err := LoadCommitMessageHook(ctx, campaignRoot)
	if err != nil {
		return "", err
	}

	return RunCommitMessageCommand(ctx, repoPath, hook.Command)
}

// RunCommitMessageCommand executes command exactly as configured from repoPath
// and returns trimmed stdout as the raw commit message.
func RunCommitMessageCommand(ctx context.Context, repoPath, command string) (string, error) {
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

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("auto-write commit message command failed: %w: %s", err, msg)
		}
		return "", fmt.Errorf("auto-write commit message command failed: %w", err)
	}

	message := strings.TrimSpace(stdout.String())
	if message == "" {
		return "", ErrCommitMessageHookEmptyOutput
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
