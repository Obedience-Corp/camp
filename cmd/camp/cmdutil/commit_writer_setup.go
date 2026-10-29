package cmdutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/autowrite"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/settings"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

const obeyCommitWriter = "ob commit --print-session-id"

// EnsureCommitWriter resolves first-use setup before staging or queuing work.
// A nonempty message is a one-time manual replacement for --auto-write.
func EnsureCommitWriter(cmd *cobra.Command, root string, interactive bool) (string, error) {
	_, err := autowrite.LoadCommitMessageHook(cmd.Context(), root)
	if !errors.Is(err, autowrite.ErrCommitMessageHookNotConfigured) {
		return "", err
	}
	if !interactive || !ui.IsTerminal() {
		return "", err
	}
	return ConfigureCommitWriter(cmd, root, true)
}

// ConfigureCommitWriter is shared by first-use commits and settings.
func ConfigureCommitWriter(cmd *cobra.Command, root string, allowManual bool) (string, error) {
	ctx := cmd.Context()
	if !ui.IsTerminal() {
		return "", camperrors.New("commit writer setup requires an interactive terminal")
	}
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return "", err
	}
	service, err := settings.NewService(ctx, root, "")
	if err != nil {
		return "", err
	}
	snapshot, err := service.Get(ctx, "campaign.yaml")
	if err != nil {
		return "", err
	}
	command := cfg.Hooks.CommitMessage.Command
	description := "Automatic commit messages aren't configured for this camp."
	if strings.TrimSpace(command) != "" {
		description = "Current command: " + command
	}
	for {
		choice := ""
		options := []huh.Option[string]{}
		if obeyAvailable(ctx, root) {
			options = append(options, huh.NewOption("Use Obey", "obey"))
		} else {
			options = append(options, huh.NewOption("Install Obey with Festival", "install"))
		}
		options = append(options, huh.NewOption("Use a custom command", "custom"))
		if allowManual {
			options = append(options, huh.NewOption("Write this commit message manually", "manual"))
		}
		options = append(options, huh.NewOption("Cancel", "cancel"))
		form := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().
			Title("Set up automatic commit messages").Description(description).
			Value(&choice).Options(options...)))
		if err := runWriterForm(ctx, form); err != nil {
			return "", err
		}
		switch choice {
		case "cancel":
			return "", git.ErrCommitCancelled
		case "manual":
			message := ""
			form := huh.NewForm(huh.NewGroup(huh.NewText().Title("Commit message").
				Description("Used for this commit only. Automatic messages stay unconfigured.").
				ExternalEditor(false).Value(&message).Validate(nonemptyWriterValue)))
			if err := runWriterForm(ctx, form); err != nil {
				return "", err
			}
			return strings.TrimSpace(message), nil
		case "install":
			installer, err := exec.LookPath("festival")
			if err != nil {
				description = "Festival isn't available. Install it from https://fest.build,\nthen run 'festival install obey', or use a custom command."
				continue
			}
			install := exec.CommandContext(ctx, installer, "install", "obey")
			install.Stdin, install.Stdout, install.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			if err := install.Run(); err != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				description = "Obey installation failed. Retry, use a custom command, or cancel."
				continue
			}
			if !obeyAvailable(ctx, root) {
				description = "The commit writer shell cannot find Obey yet.\nEnter its full path as a custom command, or cancel and check PATH."
				continue
			}
			command = obeyCommitWriter
		case "obey":
			command = obeyCommitWriter
		case "custom":
			form := huh.NewForm(huh.NewGroup(huh.NewInput().Title("Commit message command").
				Description("Runs in the target repository. Stdout becomes the commit message.\nSaved for this camp in .campaign/campaign.yaml.").
				Value(&command).Validate(nonemptyWriterValue)))
			if err := runWriterForm(ctx, form); err != nil {
				return "", err
			}
		}
		// Patch only this key; retain unknown fields, comments and writer settings.
		// The revision prevents overwriting a concurrent edit while the form was open.
		value, err := json.Marshal(strings.TrimSpace(command))
		if err != nil {
			return "", err
		}
		patch := []byte(`{"changes":[{"op":"set","path":["hooks","commit_message","command"],"value":` + string(value) + `}]}`)
		if _, err := service.Patch(ctx, "campaign.yaml", snapshot.Revision, patch); err != nil {
			return "", camperrors.Wrap(err, "saving commit message command")
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), ui.Success("Saved automatic commit messages for this camp in .campaign/campaign.yaml"))
		return "", nil
	}
}

func nonemptyWriterValue(value string) error {
	if strings.TrimSpace(value) == "" {
		return camperrors.New("Enter a nonempty value")
	}
	return nil
}

func runWriterForm(ctx context.Context, form *huh.Form) error {
	err := form.WithTheme(theme.GetTheme(theme.ThemeName(config.EffectiveTheme(ctx)))).RunWithContext(ctx)
	if theme.IsCancelled(err) {
		return git.ErrCommitCancelled
	}
	return err
}

// Resolve in the writer's shell: installer changes to shell startup files are
// visible there even when this process still has the pre-install PATH.
func obeyAvailable(ctx context.Context, root string) bool {
	lookup := "command -v ob"
	if runtime.GOOS == "windows" {
		lookup = "where ob"
	}
	_, err := autowrite.RunCommitMessageCommandWithOptions(ctx, root, lookup,
		autowrite.RunOptions{Timeout: 5 * time.Second, DiagnosticOut: io.Discard, OwnProcessGroup: true})
	return err == nil
}
