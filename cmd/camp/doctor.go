package main

import (
	"context"
	"encoding/json"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/doctor"
	"github.com/Obedience-Corp/camp/internal/doctor/checks"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose and fix campaign health issues",
	Long: `Check campaign for common issues and optionally fix them.

CHECKS PERFORMED:
  orphan      Orphaned gitlinks in index (no .gitmodules entry)
  url         URL consistency between .gitmodules and .git/config
  integrity   Submodule integrity (empty/broken directories)
  head        HEAD states (detached with local work)
  working     Working directory cleanliness
  commits     Parent-submodule commit alignment

EXIT CODES:
  0  All checks passed (no warnings or errors)
  1  Warnings found (but no errors)
  2  Errors found
  3  Fix attempted but some issues remain

EXAMPLES:
  # Run all checks
  camp doctor

  # Attempt automatic fixes
  camp doctor --fix

  # Run URL check only
  camp doctor -c url

  # Detailed output
  camp doctor --verbose

  # JSON output for scripting
  camp doctor --json`,
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Has --fix mode that is destructive",
	},
	RunE: runDoctor,
}

var doctorOpts struct {
	fix            bool
	verbose        bool
	jsonOutput     bool
	submodulesOnly bool
	checks         []string
}

func init() {
	doctorCmd.Flags().BoolVarP(&doctorOpts.fix, "fix", "f", false,
		"Attempt automatic fixes for detected issues")
	doctorCmd.Flags().BoolVarP(&doctorOpts.verbose, "verbose", "v", false,
		"Show detailed information for each check")
	doctorCmd.Flags().BoolVar(&doctorOpts.jsonOutput, "json", false,
		"Output results as JSON")
	doctorCmd.Flags().BoolVar(&doctorOpts.submodulesOnly, "submodules-only", false,
		"Only check submodule health")
	doctorCmd.Flags().StringSliceVarP(&doctorOpts.checks, "check", "c", nil,
		"Run specific check(s) only (orphan, url, integrity, head, working, commits)")

	rootCmd.AddCommand(doctorCmd)
	doctorCmd.GroupID = "campaign"
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Detect campaign root
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	// Build doctor with options
	d := doctor.NewDoctor(campRoot,
		doctor.WithFix(doctorOpts.fix),
		doctor.WithVerbose(doctorOpts.verbose),
		doctor.WithJSON(doctorOpts.jsonOutput),
		doctor.WithSubmodulesOnly(doctorOpts.submodulesOnly),
		doctor.WithChecks(doctorOpts.checks),
	)

	// Register all checks
	registerChecks(d)

	// Run doctor
	result, err := d.Run(ctx)
	if err != nil {
		return camperrors.Wrap(err, "doctor failed")
	}

	// Output results
	if doctorOpts.jsonOutput {
		return outputDoctorJSON(result)
	}

	outputDoctorText(result, doctorOpts.verbose, doctorOpts.fix)

	// Return appropriate exit code
	return exitDoctorWithCode(result)
}

// registerChecks registers all health checks with the doctor.
// OrphanCheck runs first because orphaned gitlinks can break other checks.
func registerChecks(d *doctor.Doctor) {
	d.RegisterCheck(checks.NewOrphanCheck())
	d.RegisterCheck(checks.NewURLCheck())
	d.RegisterCheck(checks.NewIntegrityCheck())
	d.RegisterCheck(checks.NewHeadCheck())
	d.RegisterCheck(checks.NewWorkingCheck())
	d.RegisterCheck(checks.NewCommitsCheck())
}

// outputDoctorJSON outputs results as JSON.
func outputDoctorJSON(result *doctor.DoctorResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// outputDoctorText outputs results in human-readable format.
func outputDoctorText(result *doctor.DoctorResult, verbose, fixAttempted bool) {
	// Header
	fmt.Println()
	fmt.Println(ui.Header("Campaign Health Report"))
	fmt.Println()

	// Summary
	total := result.Passed + result.Warned + result.Failed
	fmt.Printf("Checks: %s passed, %s warnings, %s failed (of %d)\n",
		ui.Success(fmt.Sprintf("%d", result.Passed)),
		ui.Warning(fmt.Sprintf("%d", result.Warned)),
		ui.Error(fmt.Sprintf("%d", result.Failed)),
		total)

	// Issues
	if len(result.Issues) > 0 {
		fmt.Println()
		fmt.Println(ui.Category("Issues Found:"))
		for i, issue := range result.Issues {
			icon := ui.WarningIcon()
			if issue.Severity == doctor.SeverityError {
				icon = ui.ErrorIcon()
			}
			fmt.Printf("  %d. %s %s: %s\n", i+1, icon, issue.Submodule, issue.Description)
			if verbose && issue.FixCommand != "" {
				fmt.Printf("     %s %s\n", ui.Dim("Fix:"), ui.Dim(issue.FixCommand))
			}
		}
	}

	// Fixed issues
	if len(result.Fixed) > 0 {
		fmt.Println()
		fmt.Println(ui.Category("Fixed Issues:"))
		for i, issue := range result.Fixed {
			fmt.Printf("  %d. %s %s: %s\n", i+1, ui.SuccessIcon(), issue.Submodule, issue.Description)
		}
	}

	// Summary message
	fmt.Println()
	if result.Success {
		if len(result.Issues) == 0 {
			fmt.Println(ui.Success("All checks passed."))
		} else {
			fmt.Println(ui.Warning("Warnings found but no critical errors."))
		}
	} else {
		unfixedErrors := 0
		for _, issue := range result.Issues {
			if issue.Severity == doctor.SeverityError {
				unfixedErrors++
			}
		}
		if unfixedErrors > 0 {
			fmt.Println(ui.Error(fmt.Sprintf("%d error(s) require attention.", unfixedErrors)))
		}
	}

	// Hint about fix
	if !fixAttempted && hasFixableIssues(result) {
		fmt.Println()
		fmt.Println(ui.Dim("Run with --fix to attempt automatic repairs."))
	}
}

// hasFixableIssues checks if any issues can be auto-fixed.
func hasFixableIssues(result *doctor.DoctorResult) bool {
	for _, issue := range result.Issues {
		if issue.AutoFixable {
			return true
		}
	}
	return false
}

// exitDoctorWithCode exits with appropriate code based on result.
func exitDoctorWithCode(result *doctor.DoctorResult) error {
	if result.Failed > 0 {
		os.Exit(doctor.ExitFailures)
	}

	if result.Warned > 0 {
		os.Exit(doctor.ExitWarnings)
	}

	// If fix was attempted and there are still issues
	if len(result.Fixed) > 0 && len(result.Issues) > len(result.Fixed) {
		os.Exit(doctor.ExitPartialFix)
	}

	return nil // Exit 0
}
