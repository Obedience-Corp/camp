package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/datestamp"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var dateCmd = &cobra.Command{
	Use:   "date <path>",
	Short: "Append date suffix to file or directory name",
	Long: `Append a date suffix to a file or directory name.

Renames a file or directory by appending a date to its name.
Shows a preview of the rename and asks for confirmation.

Date source (in priority order):
  --mtime    Use the file's last modified date
  --ago N    Use today minus N days
  (default)  Use today's date

Examples:
  camp date old-project              # old-project -> old-project-2026-01-27
  camp date ./docs/archive.md        # archive.md -> archive-2026-01-27.md
  camp date old-project --yes        # Skip confirmation
  camp date old-project --ago 3      # Use date from 3 days ago
  camp date old-project --mtime      # Use file's last modified date
  camp date old-project -f 20060102  # Use different date format`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeDatePath,
	RunE:              runDate,
}

func init() {
	rootCmd.AddCommand(dateCmd)
	dateCmd.GroupID = "system"

	dateCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	dateCmd.Flags().IntP("ago", "a", 0, "Use date from N days ago")
	dateCmd.Flags().BoolP("mtime", "m", false, "Use file's last modified date")
	dateCmd.Flags().StringP("format", "f", "2006-01-02", "Date format (Go time format)")
	dateCmd.Flags().Bool("dry-run", false, "Show what would be done without making changes")
}

func completeDatePath(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) >= 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveDefault
}

func runDate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	path := args[0]

	yes, _ := cmd.Flags().GetBool("yes")
	ago, _ := cmd.Flags().GetInt("ago")
	mtime, _ := cmd.Flags().GetBool("mtime")
	format, _ := cmd.Flags().GetString("format")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	opts := datestamp.Options{
		DateFormat: format,
		DaysAgo:    ago,
		UseMtime:   mtime,
		DryRun:     true, // Always preview first
	}

	// Get preview
	result, err := datestamp.Datestamp(ctx, path, opts)
	if err != nil {
		return err
	}

	// Show preview
	typeLabel := "file"
	if result.IsDirectory {
		typeLabel = "directory"
	}

	fmt.Printf("%s Rename %s:\n", ui.InfoIcon(), typeLabel)
	fmt.Printf("  %s %s %s\n",
		ui.Value(filepath.Base(result.OriginalPath)),
		ui.ArrowIcon(),
		ui.Value(filepath.Base(result.NewPath)))
	fmt.Printf("  %s\n", ui.Dim("Date: "+result.DateUsed.Format("2006-01-02")))

	if dryRun {
		fmt.Printf("\n%s Dry run - no changes made\n", ui.InfoIcon())
		return nil
	}

	// Confirm unless --yes
	if !yes {
		fmt.Printf("\nProceed? [Y/n] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response == "n" || response == "no" {
			fmt.Println(ui.Dim("Aborted."))
			return nil
		}
	}

	// Execute
	opts.DryRun = false
	result, err = datestamp.Datestamp(ctx, path, opts)
	if err != nil {
		return err
	}

	fmt.Printf("%s Renamed to %s\n", ui.SuccessIcon(), ui.Value(filepath.Base(result.NewPath)))
	return nil
}
