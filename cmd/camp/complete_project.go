package main

import (
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/spf13/cobra"
)

// completeProjectName provides tab completion for --project flags that accept
// a project name (as returned by project.List).
func completeProjectName(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	projects, err := project.List(ctx, campRoot)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []string
	for _, p := range projects {
		if strings.HasPrefix(p.Name, toComplete) {
			names = append(names, p.Name)
		}
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}
