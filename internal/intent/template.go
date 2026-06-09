package intent

import (
	"bytes"
	_ "embed"
	"text/template"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

//go:embed templates/intent.md.tmpl
var intentTemplateContent string

//go:embed templates/note.md.tmpl
var noteTemplateContent string

// intentTemplate is the parsed template, initialized on first use.
var intentTemplate *template.Template

// noteTemplate is the parsed note template, initialized on first use.
var noteTemplate *template.Template

// TemplateData contains the values to substitute into the intent template.
type TemplateData struct {
	ID        string
	Title     string
	Type      string
	Concept   string // Full concept path (e.g., "projects/camp")
	Status    string // Lifecycle/category directory (defaults to inbox)
	Author    string
	CreatedAt string // Formatted as YYYY-MM-DD
	Body      string // Description/body content
}

// RenderTemplate generates an intent file from a template with the given data.
func RenderTemplate(data TemplateData) (string, error) {
	// Parse template on first use (lazy initialization)
	if intentTemplate == nil {
		tmpl, err := template.New("intent").Parse(intentTemplateContent)
		if err != nil {
			return "", camperrors.Wrap(err, "parsing intent template")
		}
		intentTemplate = tmpl
	}

	var buf bytes.Buffer
	if err := intentTemplate.Execute(&buf, data); err != nil {
		return "", camperrors.Wrap(err, "executing intent template")
	}

	return buf.String(), nil
}

// RenderNote generates a note file from the lightweight note template. Notes
// carry no type, concept, or promotion metadata; tags organize them.
func RenderNote(data TemplateData) (string, error) {
	if noteTemplate == nil {
		tmpl, err := template.New("note").Parse(noteTemplateContent)
		if err != nil {
			return "", camperrors.Wrap(err, "parsing note template")
		}
		noteTemplate = tmpl
	}

	var buf bytes.Buffer
	if err := noteTemplate.Execute(&buf, data); err != nil {
		return "", camperrors.Wrap(err, "executing note template")
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
	status := intent.Status
	if status == "" {
		status = StatusInbox
	}
	return TemplateData{
		ID:        intent.ID,
		Title:     intent.Title,
		Type:      string(intent.Type),
		Concept:   intent.Concept,
		Status:    string(status),
		Author:    intent.Author,
		CreatedAt: FormatCreatedAt(intent.CreatedAt),
		Body:      intent.Content,
	}
}

// NewTemplateDataFromInput creates a TemplateData struct from user input.
// The timestamp is used to generate both the ID and CreatedAt fields.
func NewTemplateDataFromInput(title, typ, concept, author, body string, timestamp time.Time) TemplateData {
	return TemplateData{
		ID:        GenerateID(title, timestamp),
		Title:     title,
		Type:      typ,
		Concept:   concept,
		Status:    string(StatusInbox),
		Author:    author,
		CreatedAt: FormatCreatedAt(timestamp),
		Body:      body,
	}
}
