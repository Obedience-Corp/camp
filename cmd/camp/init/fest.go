package initcmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/fest"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// initializeFestivals runs fest init in the campaign directory.
// Returns true if successful, false with guidance if fest is unavailable.
func initializeFestivals(ctx context.Context, campaignRoot string, w Writers) (bool, error) {
	// Check if already initialized
	if fest.IsInitialized(campaignRoot) {
		writeLine(w.HumanOut, ui.Success("Festival Methodology already initialized"))
		return true, nil
	}

	// Check if fest CLI is available
	if !fest.IsFestAvailable() {
		showFestInstallGuidance(w)
		return false, fest.ErrFestNotFound
	}

	// Check if festivals directory has content but isn't initialized
	if hasNonFestContent(campaignRoot) {
		showFestManualInitGuidance(w)
		return false, nil
	}

	writeLine(w.HumanOut, ui.Info("Initializing Festival Methodology..."))
	err := fest.RunInit(ctx, &fest.InitOptions{
		CampaignRoot: campaignRoot,
	})
	if err != nil {
		showFestInitFailure(err, w)
		return false, err
	}

	writeLine(w.HumanOut, ui.Success("Festival Methodology initialized"))
	return true, nil
}

// hasNonFestContent checks if festivals/ has content that isn't fest-initialized.
func hasNonFestContent(campaignRoot string) bool {
	festivalsDir := filepath.Join(campaignRoot, "festivals")
	entries, err := os.ReadDir(festivalsDir)
	if err != nil {
		return false
	}
	// If we have entries but fest isn't initialized, there's non-fest content
	return len(entries) > 0 && !fest.IsInitialized(campaignRoot)
}

// showFestInstallGuidance displays guidance for installing fest CLI.
func showFestInstallGuidance(w Writers) {
	writeLine(w.HumanOut)
	writeLine(w.HumanOut, ui.Dim("Festival Methodology provides structured project planning."))
	writeLine(w.HumanOut, ui.Dim("Install the fest CLI to enable it:"))
	writeLine(w.HumanOut)
	writeLine(w.HumanOut, ui.Dim("  go install github.com/Obedience-Corp/fest/cmd/fest@latest"))
	writeLine(w.HumanOut)
	writeLine(w.HumanOut, ui.Dim("Then run: camp init --repair"))
	writeLine(w.HumanOut, ui.Dim("Continuing without Festival Methodology..."))
}

// showFestManualInitGuidance displays guidance when festivals/ has non-fest content.
func showFestManualInitGuidance(w Writers) {
	writeLine(w.HumanOut)
	writeLine(w.HumanOut, ui.Warning("festivals/ has content but is not fest-initialized"))
	writeLine(w.HumanOut, ui.Dim("Run 'fest init' manually to initialize, or clear the directory."))
}

// showFestInitFailure displays guidance when fest init fails.
func showFestInitFailure(err error, w Writers) {
	writeLine(w.HumanOut, ui.Warning(fmt.Sprintf("Failed to initialize Festival Methodology: %v", err)))
	writeLine(w.HumanOut)
	writeLine(w.HumanOut, ui.Dim("You may need to run 'fest init' manually."))
	writeLine(w.HumanOut, ui.Dim("Use 'fest init --help' for options."))
	writeLine(w.HumanOut, ui.Dim("Continuing with campaign creation..."))
}
