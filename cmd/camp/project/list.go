package project

import (
	"os"

	"github.com/Obedience-Corp/camp/internal/campaign"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/spf13/cobra"
)

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List projects in camp",
	Long: `List all projects in the current camp.

Projects are discovered from the projects/ directory. They may be regular
git-backed entries or linked external directories.

In a terminal, 'camp project list' (with no flags) opens an interactive
browser. Press / to filter (letters including g type into the query; j/k
move among matches; enter jumps), and from the browse list g or enter jumps
to the selected project when shell integration is loaded:

  eval "$(camp shell-init zsh)"   # or bash / sh
  camp shell-init fish | source   # fish
  camp project list               # interactive browser; g cds into the project

Piped, with --json/--count, or with a non-table --format it prints that
format instead. -i forces the browser (and still prints the table when stdout
is not a terminal).

Output formats:
  table   - Aligned columns with headers (default)
  simple  - Project names only, one per line
  json    - JSON array for scripting

Examples:
  camp project list               Browse projects (TTY) or print the table
  camp project list --json        Output as JSON
  camp project list --format json Output as JSON
  camp project list --format simple  Names only for scripting
  camp project list --count       Print only the total number of projects`,
	Aliases: []string{"ls"},
	RunE:    runProjectList,
}

var (
	projectListJSON  bool
	projectListCount bool
)

func init() {
	Cmd.AddCommand(projectListCmd)

	projectListCmd.Flags().StringP("format", "f", "table", "Output format (table, simple, json)")
	projectListCmd.Flags().BoolVar(&projectListJSON, "json", false, "Output as JSON (shorthand for --format json)")
	projectListCmd.Flags().BoolVar(&projectListCount, "count", false, "Print only the total number of projects")
	projectListCmd.Flags().BoolP("interactive", "i", false,
		"Open the interactive project browser (prints the table when stdout is not a terminal)")
	projectListCmd.Flags().String("path-output", "", "Write the selected project path to a file (shell integration)")
	_ = projectListCmd.Flags().MarkHidden("path-output")
}

func runProjectList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Detect campaign root
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	// List projects
	projects, err := projectsvc.List(ctx, root)
	if err != nil {
		return err
	}

	// Get format flag
	formatStr, _ := cmd.Flags().GetString("format")
	if projectListJSON {
		formatStr = "json"
	}
	format := projectsvc.OutputFormat(formatStr)

	if projectListCount {
		return projectsvc.FormatCount(os.Stdout, len(projects), format)
	}

	if projectListTUIRequested(cmd, stdoutIsTTY()) {
		return runProjectListTUI(cmd, root, projects)
	}

	return projectsvc.FormatProjects(os.Stdout, projects, format)
}
