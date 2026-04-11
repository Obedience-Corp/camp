package project

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/Obedience-Corp/camp/internal/ui"
)

// OutputFormat specifies the output format for project lists.
type OutputFormat string

const (
	FormatTable  OutputFormat = "table"
	FormatSimple OutputFormat = "simple"
	FormatJSON   OutputFormat = "json"
)

// FormatProjects writes projects to w in the specified format.
func FormatProjects(w io.Writer, projects []Project, format OutputFormat) error {
	switch format {
	case FormatJSON:
		return formatJSON(w, projects)
	case FormatSimple:
		return formatSimple(w, projects)
	default:
		return formatTable(w, projects)
	}
}

func formatTable(w io.Writer, projects []Project) error {
	if len(projects) == 0 {
		fmt.Fprintln(w, ui.Warning("No projects found."))
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Add one with: %s\n", ui.Accent("camp project add <url>"))
		return nil
	}

	showLinked := false
	for _, p := range projects {
		if p.Source == SourceLinked || p.Source == SourceLinkedNonGit {
			showLinked = true
			break
		}
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if showLinked {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", ui.Label("NAME"), ui.Label("PATH"), ui.Label("SOURCE"), ui.Label("TARGET"), ui.Label("TYPE"))
	} else {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", ui.Label("NAME"), ui.Label("PATH"), ui.Label("TYPE"))
	}
	for _, p := range projects {
		projectType := p.Type
		if projectType == "" {
			projectType = "-"
		}
		if showLinked {
			source := p.Source
			if source == "" {
				source = SourceSubmodule
			}
			target := p.LinkedPath
			if target == "" {
				target = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				ui.Value(p.Name),
				ui.Dim(p.Path),
				ui.Dim(source),
				ui.Dim(target),
				getProjectTypeStyled(projectType))
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", ui.Value(p.Name), ui.Dim(p.Path), getProjectTypeStyled(projectType))
	}
	return tw.Flush()
}

// getProjectTypeStyled returns styled project type text.
func getProjectTypeStyled(projectType string) string {
	switch projectType {
	case "go":
		return ui.ColoredText(projectType, ui.InfoColor) // Blue
	case "rust":
		return ui.ColoredText(projectType, ui.ErrorColor) // Red/orange for Rust
	case "typescript", "javascript":
		return ui.ColoredText(projectType, ui.WarningColor) // Yellow
	case "python":
		return ui.ColoredText(projectType, ui.SuccessColor) // Green
	default:
		return ui.Dim(projectType)
	}
}

func formatSimple(w io.Writer, projects []Project) error {
	if len(projects) == 0 {
		return nil
	}

	for _, p := range projects {
		fmt.Fprintln(w, p.Name)
	}
	return nil
}

func formatJSON(w io.Writer, projects []Project) error {
	// Ensure we output empty array instead of null
	if projects == nil {
		projects = []Project{}
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(projects)
}
