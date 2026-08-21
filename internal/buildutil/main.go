// internal/buildutil/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/internal/buildutil/tasks"
	"github.com/Obedience-Corp/camp/internal/buildutil/ui"
)

var (
	noColor bool
	verbose bool
)

// doctorStartArg turns the read-only daemon report into the one command that
// changes the machine.
const doctorStartArg = "start"

func main() {
	flag.BoolVar(&noColor, "no-color", false, "disable ANSI colours")
	flag.BoolVar(&verbose, "v", false, "verbose output")
	flag.Parse()

	// Initialize UI with color preferences
	ui.Init(noColor)

	if flag.NArg() == 0 {
		log.Fatalf("usage: buildutil <build|build-only|test|integration|integration-doctor [start]|clean|all>")
	}

	cmd := flag.Arg(0)
	startTime := time.Now()
	ctx := context.Background()

	// Hide cursor during operations
	if ui.ColourEnabled() {
		fmt.Print(ui.HideCursor)
		defer fmt.Print(ui.ShowCursor)
	}

	var err error

	switch cmd {
	case "build":
		err = tasks.Build(ctx, verbose)

	case "build-only":
		err = tasks.BuildOnly(ctx, verbose)

	case "test":
		err = tasks.Test(verbose)

	case "integration":
		err = tasks.Integration(ctx, verbose)

	case "integration-doctor":
		// `integration-doctor start` is the only writing form: it creates or
		// boots the dedicated profile. Bare, the command only reports.
		err = tasks.IntegrationDoctor(ctx, flag.Arg(1) == doctorStartArg)

	case "clean":
		err = tasks.Clean(verbose)

	case "all":
		// Run all tasks in sequence
		var errors []error

		fmt.Println("\n🧹 Cleaning...")
		if cleanErr := tasks.Clean(verbose); cleanErr != nil {
			errors = append(errors, camperrors.Newf("clean failed: %w", cleanErr))
		}

		fmt.Println("\n🔨 Building...")
		if buildErr := tasks.Build(ctx, verbose); buildErr != nil {
			// Don't continue if build fails - can't test broken code
			err = camperrors.Newf("stopping due to build failure: %w", buildErr)
			break
		}

		fmt.Println("\n🧪 Testing...")
		if testErr := tasks.Test(verbose); testErr != nil {
			errors = append(errors, camperrors.Newf("tests failed: %w", testErr))
			// Continue to integration tests even if unit tests fail
		}

		fmt.Println("\n🔗 Integration Testing...")
		if integrationErr := tasks.Integration(ctx, verbose); integrationErr != nil {
			errors = append(errors, camperrors.Newf("integration tests failed: %w", integrationErr))
		}

		// Set overall error if any step failed
		if len(errors) > 0 {
			err = camperrors.Newf("%d tasks failed", len(errors))
		}

		// Show overall summary
		if err == nil {
			totalTime := time.Since(startTime)
			cleanStatus := "✓ Complete"
			buildStatus := "✓ Complete"
			testStatus := "✓ Complete"
			integrationStatus := "✓ Complete"

			if ui.ColourEnabled() {
				cleanStatus = ui.Green + cleanStatus + ui.Reset
				buildStatus = ui.Green + buildStatus + ui.Reset
				testStatus = ui.Green + testStatus + ui.Reset
				integrationStatus = ui.Green + integrationStatus + ui.Reset
			}

			rows := [][]string{
				{"Task", "Status"},
				{"Clean", cleanStatus},
				{"Build", buildStatus},
				{"Test", testStatus},
				{"Integration", integrationStatus},
			}
			ui.SummaryCard("All Tasks Complete", rows, fmt.Sprintf("%.2fs", totalTime.Seconds()), true)
		}

	default:
		log.Fatalf("unknown command %q", cmd)
	}

	if err != nil {
		if ui.ColourEnabled() {
			fmt.Printf("\n%s✗ Error: %v%s\n", ui.Red, err, ui.Reset)
		} else {
			fmt.Printf("\nError: %v\n", err)
		}
		os.Exit(1)
	}
}
