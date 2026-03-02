package intent

import (
	"bytes"
	"errors"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"gopkg.in/yaml.v3"
)

// Parsing errors.
var (
	ErrEmptyContent       = errors.New("empty content")
	ErrInvalidFrontmatter = errors.New("invalid frontmatter format: expected two --- delimiters")
	ErrFrontmatterParse   = errors.New("failed to parse frontmatter YAML")
	ErrFrontmatterMarshal = errors.New("failed to marshal frontmatter to YAML")
)

// parsedIntent is an intermediate struct for parsing that supports both
// the new "concept" field and the legacy "project" field for backward compatibility.
type parsedIntent struct {
	ID                string           `yaml:"id"`
	Title             string           `yaml:"title"`
	Status            Status           `yaml:"status"`
	CreatedAt         time.Time        `yaml:"created_at"`
	Type              Type             `yaml:"type,omitempty"`
	Concept           string           `yaml:"concept,omitempty"`
	Project           string           `yaml:"project,omitempty"` // Legacy field - maps to Concept
	Author            string           `yaml:"author,omitempty"`
	Priority          Priority         `yaml:"priority,omitempty"`
	Horizon           Horizon          `yaml:"horizon,omitempty"`
	Tags              []string         `yaml:"tags,omitempty"`
	BlockedBy         []string         `yaml:"blocked_by,omitempty"`
	DependsOn         []string         `yaml:"depends_on,omitempty"`
	PromotionCriteria string           `yaml:"promotion_criteria,omitempty"`
	PromotedTo        string           `yaml:"promoted_to,omitempty"`
	GatheredFrom      []GatheredSource `yaml:"gathered_from,omitempty"`
	GatheredAt        time.Time        `yaml:"gathered_at,omitempty"`
	GatheredInto      string           `yaml:"gathered_into,omitempty"`
	UpdatedAt         time.Time        `yaml:"updated_at,omitempty"`
}

// toIntent converts a parsedIntent to an Intent, handling legacy field migration.
func (p *parsedIntent) toIntent() *Intent {
	intent := &Intent{
		ID:                p.ID,
		Title:             p.Title,
		Status:            p.Status,
		CreatedAt:         p.CreatedAt,
		Type:              p.Type,
		Author:            p.Author,
		Priority:          p.Priority,
		Horizon:           p.Horizon,
		Tags:              p.Tags,
		BlockedBy:         p.BlockedBy,
		DependsOn:         p.DependsOn,
		PromotionCriteria: p.PromotionCriteria,
		PromotedTo:        p.PromotedTo,
		GatheredFrom:      p.GatheredFrom,
		GatheredAt:        p.GatheredAt,
		GatheredInto:      p.GatheredInto,
		UpdatedAt:         p.UpdatedAt,
	}

	// Handle concept/project migration
	// Prefer concept if present, otherwise use legacy project
	if p.Concept != "" {
		intent.Concept = p.Concept
	} else if p.Project != "" {
		// Legacy: convert project name to concept path
		intent.Concept = "projects/" + p.Project
	}

	return intent
}

// delimiter is the frontmatter section delimiter.
var delimiter = []byte("---")

// ParseIntent parses an intent from markdown content with YAML frontmatter.
//
// Expected format:
//
//	---
//	id: example-20260119-153412
//	title: Example Intent
//	status: inbox
//	created_at: 2026-01-19
//	---
//
//	# Markdown content
//
// Returns the parsed Intent with Content field populated with the body.
func ParseIntent(content []byte) (*Intent, error) {
	// Handle empty content
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, ErrEmptyContent
	}

	// Find frontmatter delimiters
	// SplitN with limit 3 splits into at most 3 parts:
	// [before first ---] [between ---] [after second ---]
	parts := bytes.SplitN(content, delimiter, 3)
	if len(parts) < 3 {
		return nil, ErrInvalidFrontmatter
	}

	// parts[0] should be empty (or just whitespace) before first ---
	// parts[1] is frontmatter YAML
	// parts[2] is body markdown
	frontmatter := bytes.TrimSpace(parts[1])
	body := parts[2]

	// Handle case where content starts with --- but has no second delimiter
	if len(frontmatter) == 0 && len(bytes.TrimSpace(body)) == 0 {
		return nil, ErrInvalidFrontmatter
	}

	// Parse YAML into intermediate struct (supports legacy project field)
	var parsed parsedIntent
	if err := yaml.Unmarshal(frontmatter, &parsed); err != nil {
		return nil, camperrors.Wrapf(ErrFrontmatterParse, "%v", err)
	}

	// Convert to Intent (handles legacy field migration)
	intent := parsed.toIntent()

	// Store body content (trimmed of leading whitespace, preserve trailing)
	intent.Content = string(bytes.TrimLeft(body, "\n\r"))

	return intent, nil
}

// ParseIntentFromFile is a convenience function that reads and parses an intent file.
// It also sets the Path field on the returned Intent.
func ParseIntentFromFile(path string, content []byte) (*Intent, error) {
	intent, err := ParseIntent(content)
	if err != nil {
		return nil, err
	}
	intent.Path = path
	return intent, nil
}

// SerializeIntent converts an Intent struct to markdown with YAML frontmatter.
//
// The Content field is preserved as the body after the frontmatter.
// Runtime fields (Path) are not included in the output.
func SerializeIntent(intent *Intent) ([]byte, error) {
	// Marshal frontmatter to YAML
	frontmatter, err := yaml.Marshal(intent)
	if err != nil {
		return nil, camperrors.Wrapf(ErrFrontmatterMarshal, "%v", err)
	}

	// Combine frontmatter and body
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(frontmatter)
	buf.WriteString("---\n")

	// Add body content if present
	if intent.Content != "" {
		buf.WriteString("\n")
		buf.WriteString(intent.Content)
		// Ensure file ends with newline
		if !bytes.HasSuffix([]byte(intent.Content), []byte("\n")) {
			buf.WriteString("\n")
		}
	}

	return buf.Bytes(), nil
}
