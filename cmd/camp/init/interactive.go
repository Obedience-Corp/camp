package initcmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/charmbracelet/huh"
)

// checkDirectoryEmpty verifies the target directory is empty or gets user approval.
func checkDirectoryEmpty(dir string, force, isInteractive bool, w Writers) error {
	// Resolve to absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return camperrors.Wrap(err, "failed to resolve directory path")
	}

	// Check if directory exists
	info, err := os.Stat(absDir)
	if os.IsNotExist(err) {
		// Directory doesn't exist - will be created, so it's "empty"
		return nil
	}
	if err != nil {
		return camperrors.Wrap(err, "failed to check directory")
	}
	if !info.IsDir() {
		return camperrors.New(fmt.Sprintf("path exists but is not a directory: %s", absDir))
	}

	// Check if directory has contents
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return camperrors.Wrap(err, "failed to read directory")
	}

	if len(entries) == 0 {
		// Directory is empty
		return nil
	}

	// Directory is not empty
	if force {
		// User explicitly approved via --force flag
		return nil
	}

	if isInteractive {
		// Prompt for confirmation through the caller's human-facing output stream.
		writeLine(w.HumanOut, ui.Warning(fmt.Sprintf("Directory '%s' is not empty.", filepath.Base(absDir))))
		write(w.HumanOut, "Continue and initialize campaign here? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return camperrors.Wrap(err, "failed to read response")
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			return camperrors.New("initialization cancelled")
		}
		writeLine(w.HumanOut)
		return nil
	}

	// Non-interactive mode without --force
	return camperrors.New(fmt.Sprintf("directory '%s' is not empty\n       Use --force to initialize anyway, or run in an interactive terminal to confirm", filepath.Base(absDir)))
}

// collectCampaignInfo gathers description and mission from user input.
// In interactive mode, prompts via TUI. In non-interactive mode, requires flags.
func collectCampaignInfo(ctx context.Context, description, mission string, isInteractive bool) (string, string, error) {
	// If both are provided via flags, use them
	if description != "" && mission != "" {
		return description, mission, nil
	}

	// Non-interactive mode without required fields
	if !isInteractive {
		if description == "" && mission == "" {
			return "", "", camperrors.New("--description and --mission are required in non-interactive mode\n       Use -d/--description and -m/--mission flags, or run in an interactive terminal")
		}
		if description == "" {
			return "", "", camperrors.New("--description is required in non-interactive mode\n       Use -d/--description flag, or run in an interactive terminal")
		}
		if mission == "" {
			return "", "", camperrors.New("--mission is required in non-interactive mode\n       Use -m/--mission flag, or run in an interactive terminal")
		}
	}

	// Interactive mode - prompt for missing values
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Campaign Description").
				Description("A brief description of this campaign").
				Placeholder("e.g., AI agent orchestration framework").
				Value(&description),
			huh.NewText().
				Title("Mission Statement").
				Description("What is the goal of this campaign?").
				Placeholder("Describe the mission in detail...").
				CharLimit(1000).
				Value(&mission),
		),
	)

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return "", "", camperrors.New("initialization cancelled")
		}
		return "", "", camperrors.Wrap(err, "failed to collect campaign info")
	}

	// Validate that user provided values
	if description == "" {
		return "", "", camperrors.New("description is required")
	}
	if mission == "" {
		return "", "", camperrors.New("mission is required")
	}

	return description, mission, nil
}

// handleRepairMission checks for missing mission in existing campaign and prompts if needed.
func handleRepairMission(ctx context.Context, dir string, mission string, isInteractive bool, w Writers) (string, error) {
	// If mission is provided via flag, use it
	if mission != "" {
		return mission, nil
	}

	// Load existing campaign config to check for mission
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", nil // Can't check, proceed without
	}

	cfg, err := config.LoadCampaignConfig(ctx, absDir)
	if err != nil {
		return "", nil // No existing config, proceed without
	}

	// If campaign already has a mission, use it
	if cfg.Mission != "" {
		return cfg.Mission, nil
	}

	// Campaign is missing mission
	if isInteractive {
		writeLine(w.HumanOut, ui.Warning(fmt.Sprintf("Campaign '%s' is missing a mission statement.", cfg.Name)))
		writeLine(w.HumanOut)

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewText().
					Title("Mission Statement").
					Description("What is the goal of this campaign?").
					Placeholder("Describe the mission in detail...").
					CharLimit(1000).
					Value(&mission),
			),
		)

		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				// User cancelled, proceed without mission
				writeLine(w.HumanOut, ui.Dim("Skipping mission statement"))
				return "", nil
			}
			return "", camperrors.Wrap(err, "failed to collect mission")
		}

		return mission, nil
	}

	// Non-interactive mode - just warn
	writeLine(w.HumanOut, ui.Warning(fmt.Sprintf("Campaign '%s' is missing a mission statement", cfg.Name)))
	writeLine(w.HumanOut, ui.Dim("         Run 'camp init --repair' in an interactive terminal to add one"))
	writeLine(w.HumanOut)
	return "", nil
}
