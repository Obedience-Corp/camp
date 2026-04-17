package project

import (
	"os"

	"github.com/Obedience-Corp/camp/internal/campaign"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/spf13/cobra"
)

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List projects in campaign",
	Long: `List all projects in the current campaign.

Projects are discovered from the projects/ directory. They may be regular
git-backed entries or linked external directories.

Output formats:
  table   - Aligned columns with headers (default)
  simple  - Project names only, one per line
  json    - JSON array for scripting

Examples:
  camp project list               List projects in table format
  camp project list --format json Output as JSON
  camp project list --format simple  Names only for scripting`,
	Aliases: []string{"ls"},
	RunE:    runProjectList,
}

func init() {
	Cmd.AddCommand(projectListCmd)

	projectListCmd.Flags().StringP("format", "f", "table", "Output format (table, simple, json)")
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
	format := projectsvc.OutputFormat(formatStr)

	// Output projects
	return projectsvc.FormatProjects(os.Stdout, projects, format)
}
