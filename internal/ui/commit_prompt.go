package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/obediencecorp/camp/internal/git"
)

// PromptCommitMessage shows an interactive prompt for the commit message.
// Returns the message or empty string if cancelled.
func PromptCommitMessage(ctx context.Context, executor git.GitExecutor) (string, error) {
	// Show what's being committed
	showChangeSummary(ctx, executor)

	// Create the form
	var message string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title("Commit Message").
				Description("What changes are you committing?").
				Placeholder("Enter commit message...").
				CharLimit(500).
				Value(&message).
				Validate(validateCommitMessage),
		),
	)

	// Run the form
	err := form.Run()
	if err != nil {
		if err == huh.ErrUserAborted {
			return "", nil // User cancelled
		}
		return "", err
	}

	return strings.TrimSpace(message), nil
}

// PromptCommitMessageSimple shows a simple single-line prompt.
func PromptCommitMessageSimple(ctx context.Context, executor git.GitExecutor) (string, error) {
	showChangeSummary(ctx, executor)

	var message string

	input := huh.NewInput().
		Title("Commit message").
		Placeholder("Describe your changes...").
		CharLimit(120).
		Value(&message).
		Validate(validateCommitMessage)

	err := input.Run()
	if err != nil {
		if err == huh.ErrUserAborted {
			return "", nil
		}
		return "", err
	}

	return strings.TrimSpace(message), nil
}

// PromptCommitMessageMultiline shows a prompt that supports multi-line messages.
func PromptCommitMessageMultiline(ctx context.Context, executor git.GitExecutor) (string, error) {
	showChangeSummary(ctx, executor)

	var title string
	var body string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Commit Title").
				Description("First line of commit message").
				Placeholder("Short summary of changes").
				CharLimit(72). // Git convention
				Value(&title).
				Validate(validateCommitMessage),
		),
		huh.NewGroup(
			huh.NewText().
				Title("Commit Body (optional)").
				Description("Longer explanation if needed").
				Placeholder("Leave empty for single-line commit").
				CharLimit(500).
				Value(&body),
		),
	)

	err := form.Run()
	if err != nil {
		if err == huh.ErrUserAborted {
			return "", nil
		}
		return "", err
	}

	// Combine title and body
	message := strings.TrimSpace(title)
	if body = strings.TrimSpace(body); body != "" {
		message += "\n\n" + body
	}

	return message, nil
}

// validateCommitMessage ensures the message isn't empty.
func validateCommitMessage(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("commit message cannot be empty")
	}
	return nil
}

// showChangeSummary displays what will be committed.
func showChangeSummary(ctx context.Context, executor git.GitExecutor) {
	// Get diff stat for staged changes
	cmd := exec.CommandContext(ctx, "git", "-C", executor.Path(),
		"diff", "--cached", "--stat")
	output, err := cmd.Output()
	if err != nil {
		return // Non-fatal
	}

	if len(output) > 0 {
		fmt.Println("\nChanges to be committed:")
		fmt.Println(string(output))
	} else {
		// Check unstaged changes
		cmd = exec.CommandContext(ctx, "git", "-C", executor.Path(),
			"diff", "--stat")
		output, _ = cmd.Output()
		if len(output) > 0 {
			fmt.Println("\nUnstaged changes (will be staged with --all):")
			fmt.Println(string(output))
		}
	}
}
