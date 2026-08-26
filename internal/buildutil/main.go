package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	buildutil "github.com/Obedience-Corp/build-util"
	"github.com/Obedience-Corp/build-util/ui"
	"github.com/Obedience-Corp/camp/internal/buildutil/tasks"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func main() {
	args := os.Args[1:]
	ctx := context.Background()
	command := requestedCommand(args)

	root, err := findCampRepoRoot(".")
	if err != nil {
		reportError(err)
		os.Exit(1)
	}

	cfg := dashboardConfig(root)

	switch command {
	case "":
		fmt.Fprint(os.Stderr, "usage: buildutil <build|build-only|profile-commands|test|integration|integration-doctor [start]|lint|coverage|clean|all>\n")
		os.Exit(1)
	case "integration-doctor":
		ui.Init(hasFlag(args, "-no-color"))
		if err := tasks.IntegrationDoctor(ctx, doctorStartRequested(args)); err != nil {
			reportError(err)
			os.Exit(1)
		}
		return
	case "integration":
		ui.Init(hasFlag(args, "-no-color"))
		if err := tasks.Integration(ctx, hasFlag(args, "-v")); err != nil {
			reportError(err)
			os.Exit(1)
		}
		return
	case "all":
		if err := runAll(ctx, args, cfg); err != nil {
			reportError(err)
			os.Exit(1)
		}
		return
	}

	buildutil.Run(args, cfg)
}

func dashboardConfig(dir string) buildutil.BuildConfig {
	return buildutil.BuildConfig{
		BinaryName:  "camp",
		MainPath:    "./cmd/camp",
		SectionName: "Camp CLI",
		Dir:         dir,
		LDFlags:     ldflags,
		// Hang-detection safety net, not a perf gate. Some packages sit near
		// 30s on their own; keep the same headroom the local runner used.
		TestTimeout: 300 * time.Second,
	}
}

func runAll(ctx context.Context, args []string, cfg buildutil.BuildConfig) error {
	startTime := time.Now()
	verbose := hasFlag(args, "-v")
	var errs []error

	cleanCfg := cfg
	cleanCfg.SkipDockerCleanup = true
	if err := buildutil.Execute(ctx, taskArgs(args, "clean"), cleanCfg); err != nil {
		errs = append(errs, camperrors.Newf("clean failed: %w", err))
	}

	if err := buildutil.Execute(ctx, taskArgs(args, "build"), cfg); err != nil {
		return camperrors.Newf("stopping due to build failure: %w", err)
	}

	if err := buildutil.Execute(ctx, taskArgs(args, "test"), cfg); err != nil {
		errs = append(errs, camperrors.Newf("tests failed: %w", err))
	}

	ui.Init(hasFlag(args, "-no-color"))
	if err := tasks.Integration(ctx, verbose); err != nil {
		errs = append(errs, camperrors.Newf("integration tests failed: %w", err))
	}

	if len(errs) > 0 {
		return camperrors.Newf("%d tasks failed", len(errs))
	}

	ui.SummaryCard("All Tasks Complete", [][]string{
		{"Task", "Status"},
		{"Clean", ui.StyleOK("✓ Complete")},
		{"Build", ui.StyleOK("✓ Complete")},
		{"Test", ui.StyleOK("✓ Complete")},
		{"Integration", ui.StyleOK("✓ Complete")},
	}, fmt.Sprintf("%.2fs", time.Since(startTime).Seconds()), true)
	return nil
}

func requestedCommand(args []string) string {
	for _, arg := range args {
		if arg == "--" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return ""
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
	}
	return false
}

func taskArgs(args []string, task string) []string {
	out := make([]string, 0, 3)
	if hasFlag(args, "-no-color") {
		out = append(out, "-no-color")
	}
	if hasFlag(args, "-v") {
		out = append(out, "-v")
	}
	return append(out, task)
}

func doctorStartRequested(args []string) bool {
	seenCommand := false
	for _, arg := range args {
		if arg == "--" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if !seenCommand {
			seenCommand = true
			continue
		}
		return arg == "start"
	}
	return false
}

func reportError(err error) {
	fmt.Printf("\n%s\n", ui.StyleFail(fmt.Sprintf("✗ Error: %v", err)))
}
