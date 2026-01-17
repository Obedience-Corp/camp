package project

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
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
		fmt.Fprintln(w, "No projects found. Add one with: camp project add <url>")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tPATH\tTYPE")
	for _, p := range projects {
		projectType := p.Type
		if projectType == "" {
			projectType = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", p.Name, p.Path, projectType)
	}
	return tw.Flush()
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
