package cmdutil

import (
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/spf13/cobra"
)

// CompleteProjectName provides tab completion for flags and args that accept
// a project name from project.List.
func CompleteProjectName(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	projects, err := projectsvc.List(ctx, campRoot)
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
