package project

import (
	"context"
	"fmt"

	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
)

func linkProject(ctx context.Context, campaignRoot, sourcePath, name, destPath string) (*projectsvc.LinkResult, error) {
	return projectsvc.AddLinked(ctx, campaignRoot, sourcePath, projectsvc.LinkOptions{
		Name: name,
		Path: destPath,
	})
}

func printLinkedProjectResult(result *projectsvc.LinkResult) {
	fmt.Printf("%s %s\n", ui.SuccessIcon(), ui.Success("Linked project: "+result.Name))
	fmt.Println()
	fmt.Println(ui.KeyValue("  Path:", result.Path))
	fmt.Println(ui.KeyValue("  Source:", result.Source))
	if result.Type != "" {
		fmt.Println(ui.KeyValue("  Type:", result.Type))
	}
	if result.IsGit {
		fmt.Println(ui.KeyValue("  Git:", "yes"))
	} else {
		fmt.Println(ui.KeyValue("  Git:", "no"))
	}
	fmt.Println()
	fmt.Println(ui.Dim("  Linked projects are machine-local and are not auto-committed."))
}
