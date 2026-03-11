package main

import (
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectRemoteListCmd = &cobra.Command{
	Use:   "list",
	Short: "List remotes for the project",
	Long: `List all git remotes configured for the current project.

For submodule projects, also shows whether the origin URL matches
the canonical URL declared in .gitmodules.

Examples:
  camp project remote list
  camp project remote list --project my-api`,
	Aliases: []string{"ls"},
	RunE:    runProjectRemoteList,
}

func init() {
	projectRemoteCmd.AddCommand(projectRemoteListCmd)
}

func runProjectRemoteList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	resolved, err := project.Resolve(ctx, campRoot, flagRemoteProject)
	if err != nil {
		return err
	}

	remotes, err := git.ListRemotes(ctx, resolved.Path)
	if err != nil {
		return err
	}

	if len(remotes) == 0 {
		fmt.Printf("No remotes configured for project %s\n", ui.Value(resolved.Name))
		fmt.Println(ui.Dim("Add one with: camp project remote add <name> <url>"))
		return nil
	}

	isSubmodule, _ := git.IsSubmodule(resolved.Path)
	submodulePath := strings.TrimPrefix(resolved.Path, campRoot+"/")

	var urlCmp *git.URLComparison
	if isSubmodule {
		urlCmp, _ = git.CompareURLs(ctx, campRoot, submodulePath)
	}

	fmt.Printf("Remotes for %s:\n\n", ui.Value(resolved.Name))

	nameW := len("NAME")
	for _, r := range remotes {
		if len(r.Name) > nameW {
			nameW = len(r.Name)
		}
	}

	if isSubmodule {
		fmt.Printf("  %-*s  %-60s  %s\n", nameW, ui.Label("NAME"), ui.Label("URL"), ui.Label("STATUS"))
		fmt.Printf("  %s\n", ui.Dim(strings.Repeat("─", nameW+65)))
	} else {
		fmt.Printf("  %-*s  %s\n", nameW, ui.Label("NAME"), ui.Label("URL"))
		fmt.Printf("  %s\n", ui.Dim(strings.Repeat("─", nameW+62)))
	}

	for _, r := range remotes {
		url := r.FetchURL
		if url == "" {
			url = r.PushURL
		}

		if isSubmodule && r.Name == "origin" && urlCmp != nil {
			status := remoteStatus(urlCmp)
			fmt.Printf("  %-*s  %-60s  %s\n", nameW, ui.Value(r.Name), ui.Dim(url), status)

			if !urlCmp.Match && urlCmp.DeclaredURL != "" {
				fmt.Printf("  %-*s  %s %s\n", nameW, "",
					ui.Dim("canonical:"), ui.Dim(urlCmp.DeclaredURL))
			}
		} else if isSubmodule {
			fmt.Printf("  %-*s  %-60s\n", nameW, ui.Value(r.Name), ui.Dim(url))
		} else {
			fmt.Printf("  %-*s  %s\n", nameW, ui.Value(r.Name), ui.Dim(url))
		}
	}

	fmt.Println()

	if isSubmodule && urlCmp != nil && !urlCmp.Match {
		fmt.Printf("%s Origin URL drifts from .gitmodules. Fix with:\n", ui.WarningIcon())
		fmt.Printf("  camp project remote set-url %s --project %s\n",
			ui.Dim(urlCmp.DeclaredURL), resolved.Name)
		fmt.Println()
	}

	return nil
}

func remoteStatus(cmp *git.URLComparison) string {
	switch {
	case cmp.ActiveURL == "":
		return ui.Warning("not-initialized")
	case cmp.Match:
		return ui.Success("ok")
	default:
		return ui.Warning("drift")
	}
}
