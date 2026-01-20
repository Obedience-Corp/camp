package intent

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"
	"time"
)

//go:embed templates/intent.md.tmpl
var intentTemplateContent string

// intentTemplate is the parsed template, initialized on first use.
var intentTemplate *template.Template

// TemplateData contains the values to substitute into the intent template.
type TemplateData struct {
	ID        string
	Title     string
	Type      string
	Project   string
	Author    string
	CreatedAt string // Formatted as YYYY-MM-DD
}

// RenderTemplate generates an intent file from a template with the given data.
func RenderTemplate(data TemplateData) (string, error) {
	// Parse template on first use (lazy initialization)
	if intentTemplate == nil {
		tmpl, err := template.New("intent").Parse(intentTemplateContent)
		if err != nil {
			return "", fmt.Errorf("parsing intent template: %w", err)
		}
		intentTemplate = tmpl
	}

	var buf bytes.Buffer
	if err := intentTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing intent template: %w", err)
	}

	return buf.String(), nil
}

// FormatCreatedAt formats a time.Time as YYYY-MM-DD for template use.
func FormatCreatedAt(t time.Time) string {
	return t.Format("2006-01-02")
}

// NewTemplateData creates a TemplateData struct from an Intent struct.
// This is useful for re-rendering an existing intent.
func NewTemplateData(intent *Intent) TemplateData {
	return TemplateData{
		ID:        intent.ID,
		Title:     intent.Title,
		Type:      string(intent.Type),
		Project:   intent.Project,
		Author:    intent.Author,
		CreatedAt: FormatCreatedAt(intent.CreatedAt),
	}
}

// NewTemplateDataFromInput creates a TemplateData struct from user input.
// The timestamp is used to generate both the ID and CreatedAt fields.
func NewTemplateDataFromInput(title, typ, project, author string, timestamp time.Time) TemplateData {
	return TemplateData{
		ID:        GenerateID(title, timestamp),
		Title:     title,
		Type:      typ,
		Project:   project,
		Author:    author,
		CreatedAt: FormatCreatedAt(timestamp),
	}
}
