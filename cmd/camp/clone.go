package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/obediencecorp/camp/internal/clone"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var cloneCmd = &cobra.Command{
	Use:   "clone <url> [directory]",
	Short: "Clone a campaign with full submodule setup",
	Long: `Clone a campaign repository and initialize all submodules.

This command provides a single-step setup for new devices:

  1. CLONE REPOSITORY
     Clones the campaign repository with recursive submodules.

  2. SYNCHRONIZE URLs
     Copies URLs from .gitmodules to .git/config, ensuring
     URL consistency across all submodules.

  3. UPDATE SUBMODULES
     Fetches and checks out the correct commits for all submodules.

  4. VALIDATE SETUP
     Verifies all submodules are initialized, at correct commits,
     and have matching URLs.

EXIT CODES:
  0  Success
  1  Clone failed (no campaign created)
  2  Partial success (some submodules failed)
  3  Validation failed
  4  Invalid arguments

EXAMPLES:
  # Clone a campaign (default: SSH)
  camp clone git@github.com:Obedience-Corp/obey-campaign.git

  # Clone with HTTPS
  camp clone https://github.com/Obedience-Corp/obey-campaign.git

  # Clone to a specific directory
  camp clone git@github.com:org/repo.git my-campaign

  # Clone a specific branch
  camp clone git@github.com:org/repo.git --branch develop

  # Shallow clone (latest commit only)
  camp clone git@github.com:org/repo.git --depth 1

  # Clone without submodules
  camp clone git@github.com:org/repo.git --no-submodules

  # Clone without validation
  camp clone git@github.com:org/repo.git --no-validate

  # JSON output for scripting
  camp clone git@github.com:org/repo.git --json`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runClone,
}

var cloneOpts struct {
	branch       string
	depth        int
	noSubmodules bool
	noValidate   bool
	verbose      bool
	json         bool
}

func init() {
	cloneCmd.Flags().StringVarP(&cloneOpts.branch, "branch", "b", "",
		"Clone specific branch (default: repository default branch)")
	cloneCmd.Flags().IntVar(&cloneOpts.depth, "depth", 0,
		"Shallow clone depth (0 = full history)")
	cloneCmd.Flags().BoolVar(&cloneOpts.noSubmodules, "no-submodules", false,
		"Skip submodule initialization")
	cloneCmd.Flags().BoolVar(&cloneOpts.noValidate, "no-validate", false,
		"Skip post-clone validation")
	cloneCmd.Flags().BoolVarP(&cloneOpts.verbose, "verbose", "v", false,
		"Show detailed output for each operation")
	cloneCmd.Flags().BoolVar(&cloneOpts.json, "json", false,
		"Output results as JSON for scripting")

	rootCmd.AddCommand(cloneCmd)
}

func runClone(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	url := args[0]
	var directory string
	if len(args) > 1 {
		directory = args[1]
	}

	// Create progress reporter
	var progress clone.ProgressReporter
	if cloneOpts.json {
		progress = &clone.SilentReporter{}
	} else {
		progress = clone.NewConsoleReporter(os.Stdout, cloneOpts.verbose)
	}

	// Create cloner with options
	cloner := clone.NewCloner(
		clone.WithURL(url),
		clone.WithDirectory(directory),
		clone.WithBranch(cloneOpts.branch),
		clone.WithDepth(cloneOpts.depth),
		clone.WithNoSubmodules(cloneOpts.noSubmodules),
		clone.WithNoValidate(cloneOpts.noValidate),
		clone.WithVerbose(cloneOpts.verbose),
		clone.WithJSON(cloneOpts.json),
		clone.WithProgress(progress),
	)

	// Execute clone
	result, err := cloner.Clone(ctx)
	if err != nil {
		formatCloneError(err, cloneOpts.json)
		os.Exit(clone.ExitCloneFailed)
	}

	// Format and display output
	if cloneOpts.json {
		jsonBytes, jsonErr := result.JSON()
		if jsonErr != nil {
			fmt.Fprintln(os.Stderr, string(clone.JSONError(jsonErr)))
			os.Exit(clone.ExitCloneFailed)
		}
		fmt.Println(string(jsonBytes))
	} else {
		formatCloneHuman(result, cloneOpts.verbose)
	}

	// Return appropriate exit code
	return determineCloneExitCode(result)
}

// formatCloneError formats a clone error for output.
func formatCloneError(err error, jsonOutput bool) {
	if jsonOutput {
		fmt.Fprintln(os.Stderr, string(clone.JSONError(err)))
	} else {
		// Check for SSH errors
		errStr := err.Error()
		if strings.Contains(errStr, "Permission denied (publickey)") ||
			strings.Contains(errStr, "Host key verification failed") {
			fmt.Fprintln(os.Stderr, clone.FormatSSHError(err))
		} else {
			fmt.Fprintf(os.Stderr, "%s Clone failed: %v\n", ui.ErrorIcon(), err)
		}
	}
}

// formatCloneHuman displays human-readable clone output using ui package.
func formatCloneHuman(result *clone.CloneResult, verbose bool) {
	fmt.Println()

	// Header
	if result.Success {
		fmt.Println(ui.Success("Campaign cloned successfully"))
	} else {
		fmt.Println(ui.Error("Clone completed with issues"))
	}
	fmt.Println()

	// Directory and branch
	fmt.Printf("  Directory: %s\n", result.Directory)
	if result.Branch != "" {
		fmt.Printf("  Branch:    %s\n", result.Branch)
	}
	fmt.Println()

	// Submodules section
	if len(result.Submodules) > 0 {
		successCount := 0
		for _, sub := range result.Submodules {
			if sub.Success {
				successCount++
			}
		}

		fmt.Printf("Submodules: %d/%d initialized\n", successCount, len(result.Submodules))
		if verbose {
			for _, sub := range result.Submodules {
				if sub.Success {
					fmt.Printf("  %s %s\n", ui.SuccessIcon(), sub.Path)
				} else {
					fmt.Printf("  %s %s - %v\n", ui.ErrorIcon(), sub.Path, sub.Error)
				}
			}
		}
		fmt.Println()
	}

	// URL changes section
	if len(result.URLChanges) > 0 {
		fmt.Printf("URL Synchronization: %d URLs updated\n", len(result.URLChanges))
		if verbose {
			for _, change := range result.URLChanges {
				fmt.Printf("  %s %s\n", ui.WarningIcon(), change.Submodule)
				fmt.Printf("      old: %s\n", change.OldURL)
				fmt.Printf("      new: %s\n", change.NewURL)
			}
		}
		fmt.Println()
	}

	// Validation section
	if result.Validation != nil {
		if result.Validation.Passed {
			fmt.Println("Validation: " + ui.SuccessIcon() + " All checks passed")
		} else {
			fmt.Println("Validation: " + ui.ErrorIcon() + " Issues detected")
			for _, issue := range result.Validation.Issues {
				var icon string
				switch issue.Severity {
				case clone.SeverityError:
					icon = ui.ErrorIcon()
				case clone.SeverityWarning:
					icon = ui.WarningIcon()
				default:
					icon = ui.InfoIcon()
				}
				fmt.Printf("  %s [%s] %s\n", icon, issue.Submodule, issue.Description)
				if issue.FixCommand != "" && verbose {
					fmt.Printf("      Fix: %s\n", issue.FixCommand)
				}
			}
		}
		fmt.Println()
	}

	// Warnings section
	if len(result.Warnings) > 0 && verbose {
		fmt.Println("Warnings:")
		for _, warning := range result.Warnings {
			fmt.Printf("  %s %s\n", ui.WarningIcon(), warning)
		}
		fmt.Println()
	}

	// Errors section
	if len(result.Errors) > 0 {
		fmt.Println("Errors:")
		for _, err := range result.Errors {
			fmt.Printf("  %s %s\n", ui.ErrorIcon(), err.Error())
		}
		fmt.Println()
	}
}

// determineCloneExitCode sets the appropriate exit code based on result.
func determineCloneExitCode(result *clone.CloneResult) error {
	if result.Success {
		return nil
	}

	// Check for partial failure (some submodules failed)
	if len(result.Submodules) > 0 {
		failed := 0
		for _, sub := range result.Submodules {
			if !sub.Success {
				failed++
			}
		}
		if failed > 0 && failed < len(result.Submodules) {
			os.Exit(clone.ExitPartialSuccess)
		}
	}

	// Check for validation failure
	if result.Validation != nil && !result.Validation.Passed {
		os.Exit(clone.ExitValidationFailed)
	}

	// General failure
	os.Exit(clone.ExitCloneFailed)
	return nil
}
