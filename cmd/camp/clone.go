package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/clone"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/peer"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var cloneCmd = &cobra.Command{
	Use:   "clone <url> [directory]",
	Short: "Clone a camp with full submodule setup",
	Long: `Clone a camp repository and initialize all submodules.

This command provides a single-step setup for new devices:

  1. CLONE REPOSITORY
     Clones the camp repository, then initializes submodules.

  2. SYNCHRONIZE URLs
     Copies URLs from .gitmodules to .git/config, ensuring
     URL consistency across all submodules.

  3. UPDATE SUBMODULES
     Selects each submodule's declared branch (or remote default)
     before discovering children in that checkout.

  4. VALIDATE SETUP
     Verifies declared submodules are initialized and checks URLs
     and recorded gitlinks against the selected checkouts.
     Reports declarations without gitlinks and gitlinks without
     declarations separately for each owning repository.

  5. REGISTER CAMP
     If .campaign/campaign.yaml exists, registers the camp
     in the global registry for navigation and discovery.

JSON results include a nested child tree and attempted/skipped/initialized
counts when recursive submodules are declared. Validation keeps its boolean
checks and adds checkStates (passed, failed, skipped, or unavailable).
With --no-submodules, validation reports skipped checks.
The initialized count is verified top-level checkouts; succeeded counts
complete top-level subtrees, and failed excludes cancelled skips.
Normal output shows each failed module and its original Git error.
--no-validate skips checks but still reports failed initialization and exits 3
when the root clone is usable. --no-submodules intentionally skips setup.

EXIT CODES:
  0  Success
  1  Runtime failure (clone failed before usable camp)
  2  Usage error (bad flags or args)
  3  Partial success or validation failed

EXAMPLES:
  # Clone a camp (default: SSH)
  camp clone git@github.com:Obedience-Corp/obey-campaign.git

  # Clone with HTTPS
  camp clone https://github.com/Obedience-Corp/obey-campaign.git

  # Clone to a specific directory
  camp clone git@github.com:org/repo.git my-camp

  # Clone a specific branch
  camp clone git@github.com:org/repo.git --branch develop

  # Shallow clone (latest commit only)
  camp clone git@github.com:org/repo.git --depth 1

  # Clone without submodules
  camp clone git@github.com:org/repo.git --no-submodules

  # Clone without validation
  camp clone git@github.com:org/repo.git --no-validate

  # Clone without auto-registration
  camp clone git@github.com:org/repo.git --no-register

  # JSON output for scripting
  camp clone git@github.com:org/repo.git --json

  # Seed from a peer machine in ~/.obey/machines.yaml: root repo and
  # submodules clone from that machine's copy (LAN/tailnet), then origin is
  # re-pointed to the URL above and the delta fetched. The result is an
  # origin replica that arrived over the fast path; peer failures fall back
  # to plain origin cloning.
  camp clone git@github.com:org/repo.git --from studio-mac

On Windows, --from peer seeding is unavailable in this release. Omit --from
to clone directly from origin.`,
	Args: jsoncontract.Args(CloneJSONVersion, func() bool { return cloneOpts.json }, cobra.RangeArgs(1, 2)),
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Clones repos, needs human judgment on URL/SSH",
	},
	RunE: jsoncontract.RunE(CloneJSONVersion, func() bool { return cloneOpts.json }, runClone),
}

const CloneJSONVersion = "clone/v1alpha1"

var cloneOpts struct {
	branch       string
	depth        int
	parallel     int
	noSubmodules bool
	noValidate   bool
	noRegister   bool
	verbose      bool
	json         bool
	from         string
}

func init() {
	cloneCmd.Flags().StringVarP(&cloneOpts.branch, "branch", "b", "",
		"Clone specific branch (default: repository default branch)")
	cloneCmd.Flags().IntVar(&cloneOpts.depth, "depth", 0,
		"Shallow clone depth (0 = full history)")
	cloneCmd.Flags().IntVarP(&cloneOpts.parallel, "parallel", "p", 4,
		"Maximum initialization workers; operations sharing a superproject are serialized to avoid Git config/index contention")
	cloneCmd.Flags().BoolVar(&cloneOpts.noSubmodules, "no-submodules", false,
		"Skip submodule initialization")
	cloneCmd.Flags().BoolVar(&cloneOpts.noValidate, "no-validate", false,
		"Skip post-clone validation")
	cloneCmd.Flags().BoolVar(&cloneOpts.noRegister, "no-register", false,
		"Skip auto-registration in global camp registry")
	cloneCmd.Flags().BoolVarP(&cloneOpts.verbose, "verbose", "v", false,
		"Show detailed output for each operation")
	cloneCmd.Flags().BoolVar(&cloneOpts.json, "json", false,
		"Output results as JSON for scripting")
	cloneCmd.Flags().StringVar(&cloneOpts.from, "from", "",
		"Seed git objects from this machine (id from ~/.obey/machines.yaml), then fetch the delta from origin")

	rootCmd.AddCommand(cloneCmd)
	cloneCmd.GroupID = "setup"
	cloneCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(CloneJSONVersion, func() bool { return cloneOpts.json }))
}

func runClone(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if cloneOpts.from != "" {
		if err := remote.RequirePeerSupport(); err != nil {
			return camperrors.Wrap(err, "camp clone --from")
		}
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
	clonerOpts := []clone.ClonerOption{
		clone.WithURL(url),
		clone.WithDirectory(directory),
		clone.WithBranch(cloneOpts.branch),
		clone.WithDepth(cloneOpts.depth),
		clone.WithParallel(cloneOpts.parallel),
		clone.WithNoSubmodules(cloneOpts.noSubmodules),
		clone.WithNoValidate(cloneOpts.noValidate),
		clone.WithNoRegister(cloneOpts.noRegister),
		clone.WithVerbose(cloneOpts.verbose),
		clone.WithJSON(cloneOpts.json),
		clone.WithProgress(progress),
	}
	if cloneOpts.from != "" {
		// The campaign is looked up on the peer by the name this clone will
		// register under: the directory argument when given, else the repo
		// name from the URL.
		name := clone.RepoNameFromURL(url)
		if directory != "" {
			name = filepath.Base(directory)
		}
		src, err := peer.FromMachine(ctx, cloneOpts.from, name)
		switch {
		case err != nil && errors.Is(err, peer.ErrPeerConfig):
			// Misconfiguration or typo (unknown machine, bad auth): fail fast
			// so the user fixes it, rather than silently ignoring --from.
			formatCloneError(err, cloneOpts.json)
			return camperrors.NewCommand("camp clone", clone.ExitCloneFailed, "", err)
		case err != nil:
			// Reachability failure: help text promises fallback to a plain
			// origin clone. Warn and clone from origin with no peer configured.
			if !cloneOpts.json {
				fmt.Fprintf(os.Stderr, "%s peer %q unreachable (%v); cloning from origin\n",
					ui.WarningIcon(), cloneOpts.from, err)
			}
			// The warning above is suppressed in JSON mode, so carry the
			// degradation into the machine-readable output instead of letting
			// it vanish.
			clonerOpts = append(clonerOpts, clone.WithPeerUnavailable(
				fmt.Sprintf("peer %q unreachable: %v", cloneOpts.from, err)))
		default:
			clonerOpts = append(clonerOpts, clone.WithPeer(src))
		}
	}
	cloner := clone.NewCloner(clonerOpts...)

	// Execute clone
	result, err := cloner.Clone(ctx)
	if err != nil {
		formatCloneError(err, cloneOpts.json)
		return camperrors.NewCommand("camp clone", clone.ExitCloneFailed, "", err)
	}

	// Format and display output
	if cloneOpts.json {
		jsonBytes, jsonErr := result.JSON()
		if jsonErr != nil {
			fmt.Fprintln(os.Stderr, string(clone.JSONError(jsonErr)))
			return camperrors.NewCommand("camp clone", clone.ExitCloneFailed, "", jsonErr)
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
		fmt.Println(ui.Success("Your camp was cloned successfully"))
	} else {
		fmt.Fprintln(os.Stderr, ui.Error("Clone completed with issues"))
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
		initialized, failed, skipped := 0, 0, 0
		for _, sub := range result.Submodules {
			if sub.Initialized {
				initialized++
			}
			if sub.Skipped {
				skipped++
			} else if !sub.Success {
				failed++
			}
		}
		fmt.Printf("Submodules: %d/%d initialized", initialized, len(result.Submodules))
		if failed > 0 || skipped > 0 {
			fmt.Printf(" (%d failed, %d skipped)", failed, skipped)
		}
		fmt.Println()
		for _, sub := range result.Submodules {
			if sub.Success {
				if verbose {
					fmt.Printf("  %s %s\n", ui.SuccessIcon(), sub.Path)
				}
				continue
			}
			if sub.Skipped {
				fmt.Fprintf(os.Stderr, "  %s %s (skipped): %v\n", ui.WarningIcon(), sub.Path, sub.Error)
			} else if len(sub.Nested) > 0 {
				fmt.Fprintf(os.Stderr, "  %s %s (children failed)\n", ui.ErrorIcon(), sub.Path)
				formatCloneNestedFailures(sub.Nested)
			} else {
				fmt.Fprintf(os.Stderr, "  %s %s (%s): %v\n", ui.ErrorIcon(), sub.Path, sub.Operation, sub.Error)
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
		if result.Validation.CheckStates["allInitialized"] == "skipped" {
			fmt.Println("Validation: skipped (--no-submodules)")
		} else if result.Validation.Passed {
			fmt.Println("Validation: " + ui.SuccessIcon() + " All checks passed")
		} else {
			fmt.Println("Validation: " + ui.ErrorIcon() + " Issues detected")
		}
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
			if issue.FixCommand != "" {
				fmt.Printf("      Fix: %s\n", issue.FixCommand)
			}
		}
		fmt.Println()
	}

	// Registration section
	if result.Registration != nil {
		if result.Registration.Registered {
			fmt.Printf("Registration: %s Registered as %q\n", ui.SuccessIcon(), result.Registration.CampaignName)
		} else if result.Registration.Error != nil {
			fmt.Printf("Registration: %s %v\n", ui.WarningIcon(), result.Registration.Error)
		}
		fmt.Println()
	}
	if len(result.OptionalLinks) > 0 {
		fmt.Printf("Optional project links: %d unresolved (local setup skipped)\n", len(result.OptionalLinks))
		for _, link := range result.OptionalLinks {
			fmt.Printf("  %s %s → %s\n", ui.WarningIcon(), link.Path, link.Target)
		}
		fmt.Println()
	}

	// Warnings section
	if len(result.Warnings) > 0 {
		fmt.Println("Warnings:")
		for _, warning := range result.Warnings {
			fmt.Printf("  %s %s\n", ui.WarningIcon(), warning)
		}
		fmt.Println()
	}

	// Errors section
	var unshownErrors []error
	for _, err := range result.Errors {
		shown := false
		for _, sub := range result.Submodules {
			if sub.Error != nil && err.Error() == sub.Error.Error() {
				shown = true
				break
			}
		}
		if !shown {
			unshownErrors = append(unshownErrors, err)
		}
	}
	if len(unshownErrors) > 0 {
		fmt.Println("Errors:")
		for _, err := range unshownErrors {
			fmt.Printf("  %s %s\n", ui.ErrorIcon(), err.Error())
		}
		fmt.Println()
	}
}

func formatCloneNestedFailures(results []git.NestedResult) {
	for _, nested := range results {
		if nested.Skipped {
			fmt.Fprintf(os.Stderr, "    %s %s (skipped): %v\n", ui.WarningIcon(), nested.Path, nested.Error)
		} else if nested.Error != nil && (nested.Operation != "children" || len(nested.Children) == 0) {
			fmt.Fprintf(os.Stderr, "    %s %s (owner %s, %s): %v\n", ui.ErrorIcon(), nested.Path, nested.Owner, nested.Operation, nested.Error)
		}
		formatCloneNestedFailures(nested.Children)
	}
}

// determineCloneExitCode sets the appropriate exit code based on result.
func determineCloneExitCode(result *clone.CloneResult) error {
	if result.Success {
		return nil
	}
	if result.Directory == "" {
		return camperrors.NewCommand("camp clone", clone.ExitCloneFailed, "", nil)
	}

	// A usable root with any failed required module is a partial clone, even
	// when every module failed or validation was deliberately skipped.
	if len(result.Submodules) > 0 {
		for _, sub := range result.Submodules {
			if !sub.Success {
				return camperrors.NewCommand("camp clone", clone.ExitPartialSuccess, "", nil)
			}
		}
	}

	// Check for validation failure
	if result.Validation != nil && !result.Validation.Passed {
		return camperrors.NewCommand("camp clone", clone.ExitValidationFailed, "", nil)
	}

	// The cloned root is usable even if setup failed before a module result
	// could be recorded (for example, malformed metadata).
	return camperrors.NewCommand("camp clone", clone.ExitPartialSuccess, "", nil)
}
