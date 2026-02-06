package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/git"
	"github.com/obediencecorp/camp/internal/transfer"
	"github.com/spf13/cobra"
)

var transferCmd = &cobra.Command{
	Use:   "transfer <src> <dest>",
	Short: "Copy or move files between projects",
	Long: `Transfer files between campaign projects using project:path syntax.

Default behavior is copy. Use --move to move instead.
Both source and destination support "project:path" notation.
Plain paths are resolved relative to the current directory.`,
	Example: `  camp transfer fest:README.md camp:docs/fest-readme.md
  camp transfer fest:cmd/main.go ./local-copy.go
  camp transfer --move old-project:file.txt new-project:file.txt`,
	Args: cobra.ExactArgs(2),
	RunE: runTransfer,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 2 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeTransferArg(cmd, toComplete)
	},
}

func init() {
	rootCmd.AddCommand(transferCmd)
	transferCmd.GroupID = "project"
	transferCmd.Flags().Bool("move", false, "Move file instead of copying")
	transferCmd.Flags().BoolP("force", "f", false, "Overwrite destination without prompting")
}

func runTransfer(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	moveMode, _ := cmd.Flags().GetBool("move")
	force, _ := cmd.Flags().GetBool("force")

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	srcPath, err := transfer.ResolveCampaignPath(ctx, root, args[0])
	if err != nil {
		return fmt.Errorf("resolve source: %w", err)
	}
	if err := transfer.ValidatePathExists(srcPath); err != nil {
		return fmt.Errorf("source: %w", err)
	}

	destPath, err := transfer.ResolveCampaignPath(ctx, root, args[1])
	if err != nil {
		return fmt.Errorf("resolve destination: %w", err)
	}

	// Check if destination exists and prompt for overwrite
	if !force {
		if _, err := os.Stat(destPath); err == nil {
			return fmt.Errorf("destination %q already exists (use --force to overwrite)", destPath)
		}
	}

	// Create destination directories
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	if moveMode {
		if err := os.Rename(srcPath, destPath); err != nil {
			// Cross-device move: copy then remove
			if err := copyFile(srcPath, destPath); err != nil {
				return fmt.Errorf("move (copy phase): %w", err)
			}
			if err := os.Remove(srcPath); err != nil {
				return fmt.Errorf("move (remove source): %w", err)
			}
		}
		fmt.Printf("Moved %s → %s\n", args[0], args[1])
	} else {
		if err := copyFile(srcPath, destPath); err != nil {
			return fmt.Errorf("copy: %w", err)
		}
		fmt.Printf("Copied %s → %s\n", args[0], args[1])
	}
	return nil
}

// copyFile copies a single file from src to dest, preserving permissions.
func copyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	info, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	destFile, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return fmt.Errorf("write data: %w", err)
	}
	return nil
}

// completeTransferArg provides tab completion for project:path arguments.
func completeTransferArg(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	// Check if we're completing after the colon (file paths)
	if idx := strings.Index(toComplete, ":"); idx >= 0 {
		projectName := toComplete[:idx]
		pathPrefix := toComplete[idx+1:]
		return completeProjectPath(cmd, root, projectName, pathPrefix, toComplete[:idx+1])
	}

	// Before colon: complete project names with trailing colon
	matches, err := git.ListSubmodulePathsFiltered(ctx, root, toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, filepath.Base(m)+":")
	}
	return names, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

// completeProjectPath completes file paths within a resolved project directory.
func completeProjectPath(cmd *cobra.Command, root, projectName, pathPrefix, colonPrefix string) ([]string, cobra.ShellCompDirective) {
	// Resolve project to absolute path
	all, err := git.ListSubmodulePaths(cmd.Context(), root)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var projectDir string
	nameLower := strings.ToLower(projectName)
	for _, p := range all {
		if strings.ToLower(filepath.Base(p)) == nameLower {
			projectDir = filepath.Join(root, p)
			break
		}
	}
	if projectDir == "" {
		return nil, cobra.ShellCompDirectiveError
	}

	// List entries in the target directory
	searchDir := filepath.Join(projectDir, pathPrefix)
	dirToRead := searchDir
	prefix := pathPrefix

	// If pathPrefix doesn't end with /, treat last component as a prefix filter
	var filter string
	if pathPrefix != "" && !strings.HasSuffix(pathPrefix, "/") {
		dirToRead = filepath.Dir(searchDir)
		filter = filepath.Base(pathPrefix)
		prefix = filepath.Dir(pathPrefix)
		if prefix == "." {
			prefix = ""
		} else {
			prefix += "/"
		}
	}

	entries, err := os.ReadDir(dirToRead)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var completions []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if filter != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(filter)) {
			continue
		}
		suffix := ""
		if e.IsDir() {
			suffix = "/"
		}
		completions = append(completions, colonPrefix+prefix+name+suffix)
	}
	return completions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}
