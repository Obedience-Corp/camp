package project

import (
	"errors"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"strings"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/ktr0731/go-fuzzyfinder"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var projectRunCmd = &cobra.Command{
	Use:   "run [--project <name>] [--] <command> [args...]",
	Short: "Run a command inside a project directory",
	Long: `Run any shell command inside a project directory from anywhere in the campaign.

The project is resolved in this order:
  1. --project / -p flag (explicit project name)
  2. Auto-detect from current working directory
  3. Interactive fuzzy picker (if neither above applies)

Use -- to separate camp flags from the command to execute.

Examples:
  # Interactive project picker, then run command
  camp project run -- ls -la

  # Specify project explicitly
  camp project run -p fest -- just build
  camp project run --project camp -- go test ./...

  # Auto-detect from cwd (inside projects/fest/)
  camp project run -- just test all

  # Simple commands (no -- needed when no flags)
  camp project run make build`,
	DisableFlagParsing: true,
	RunE:               runProjectRun,
}

func init() {
	Cmd.AddCommand(projectRunCmd)
}

func runProjectRun(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Handle --help/-h manually since DisableFlagParsing is true.
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return cmd.Help()
		}
		if a == "--" {
			break
		}
	}

	// Parse --project/-p flag manually since DisableFlagParsing is true.
	projectName, commandArgs := parseProjectRunArgs(args)

	// Detect campaign root.
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	// Resolve project directory.
	var projectDir string
	displayPath := ""

	switch {
	case projectName != "":
		resolved, err := projectsvc.Resolve(ctx, campRoot, projectName)
		if err != nil {
			var notFound *projectsvc.ProjectNotFoundError
			if errors.As(err, &notFound) {
				fmt.Println(ui.Dim("\n" + projectsvc.FormatProjectList(notFound.AvailableProjects())))
			}
			return err
		}
		projectDir = resolved.Path
		displayPath = resolved.LogicalPath

	default:
		// Try auto-detect from cwd first.
		result, cwdErr := projectsvc.ResolveFromCwd(ctx, campRoot)
		if cwdErr == nil {
			projectDir = result.Path
			displayPath = result.LogicalPath
		} else {
			// Fall back to interactive picker.
			picked, pickErr := pickProject(cmd, campRoot)
			if pickErr != nil {
				return pickErr
			}
			projectDir = projectsvc.ResolveProjectPath(campRoot, *picked)
			displayPath = picked.Path
		}
	}

	if len(commandArgs) == 0 {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "no command specified")
	}

	// Show which project we're running in.
	if displayPath == "" {
		displayPath = projectDir
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", ui.Dim("project:"), ui.Value(displayPath))

	// Execute command in the project directory.
	fullCmd := strings.Join(commandArgs, " ")
	return cmdutil.ExecuteCommand(ctx, fullCmd, projectDir, campRoot, nil)
}

// parseProjectRunArgs extracts --project/-p from args and returns the project
// name and remaining command args. Handles -- as explicit separator.
func parseProjectRunArgs(args []string) (projectName string, command []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]

		// -- separator: everything after is the command.
		if arg == "--" {
			return projectName, args[i+1:]
		}

		// --project=value or -p=value
		if strings.HasPrefix(arg, "--project=") {
			projectName = strings.TrimPrefix(arg, "--project=")
			continue
		}
		if strings.HasPrefix(arg, "-p=") {
			projectName = strings.TrimPrefix(arg, "-p=")
			continue
		}

		// --project value or -p value
		if (arg == "--project" || arg == "-p") && i+1 < len(args) {
			i++
			projectName = args[i]
			continue
		}

		// Not a flag — this and everything after is the command.
		return projectName, args[i:]
	}

	return projectName, nil
}

// pickProject launches an interactive fuzzy finder for project selection.
func pickProject(cmd *cobra.Command, campRoot string) (*projectsvc.Project, error) {
	ctx := cmd.Context()

	projects, err := projectsvc.List(ctx, campRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to list projects")
	}
	if len(projects) == 0 {
		return nil, camperrors.Wrap(camperrors.ErrNotFound, "no projects found in campaign")
	}

	idx, err := fuzzyfinder.Find(
		projects,
		func(i int) string {
			p := projects[i]
			if p.Type != "" {
				return fmt.Sprintf("%s [%s]", p.Name, p.Type)
			}
			return p.Name
		},
		fuzzyfinder.WithPreviewWindow(func(i, w, h int) string {
			if i < 0 || i >= len(projects) {
				return ""
			}
			return formatProjectPreview(projects[i])
		}),
		fuzzyfinder.WithPromptString("Select project: "),
		fuzzyfinder.WithHeader("  ↑/↓ navigate • type to filter • esc cancel"),
		fuzzyfinder.WithContext(ctx),
	)
	if err != nil {
		if errors.Is(err, fuzzyfinder.ErrAbort) {
			return nil, camperrors.Wrap(camperrors.ErrCancelled, "cancelled")
		}
		return nil, camperrors.Wrap(err, "picker")
	}

	return &projects[idx], nil
}

// formatProjectPreview renders preview info for a project in the fuzzy picker.
func formatProjectPreview(p projectsvc.Project) string {
	var b strings.Builder
	pad := "  "

	b.WriteString(fmt.Sprintf("%s%s\n", pad, p.Name))

	if p.Type != "" {
		b.WriteString(fmt.Sprintf("%sType: %s\n", pad, p.Type))
	}

	b.WriteString(fmt.Sprintf("%sPath: %s\n", pad, p.Path))

	source := p.Source
	if source == "" {
		source = projectsvc.SourceSubmodule
	}
	b.WriteString(fmt.Sprintf("%sSource: %s\n", pad, source))

	if p.LinkedPath != "" {
		b.WriteString(fmt.Sprintf("%sTarget: %s\n", pad, p.LinkedPath))
	}

	if p.URL != "" {
		b.WriteString(fmt.Sprintf("%sRemote: %s\n", pad, p.URL))
	}

	if p.MonorepoRoot != "" {
		b.WriteString(fmt.Sprintf("%sMonorepo: %s\n", pad, p.MonorepoRoot))
	}

	return b.String()
}
