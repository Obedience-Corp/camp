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
	AssignedTo        string           `yaml:"assigned_to,omitempty"`
	AssignedAt        time.Time        `yaml:"assigned_at,omitempty"`
	WorkRef           []string         `yaml:"work_ref,omitempty"`
	frontmatterExtras []frontmatterEntry
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
		AssignedTo:        p.AssignedTo,
		AssignedAt:        p.AssignedAt,
		WorkRef:           p.WorkRef,
		frontmatterExtras: p.frontmatterExtras,
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

// Frontmatter delimiters must be on their own lines.
var (
	frontmatterStart     = []byte("---\n")
	frontmatterDelimiter = []byte("\n---\n")
)

type frontmatterEntry struct {
	Key   yaml.Node
	Value yaml.Node
}

var knownFrontmatterKeys = map[string]struct{}{
	"id":                 {},
	"title":              {},
	"status":             {},
	"created_at":         {},
	"type":               {},
	"concept":            {},
	"project":            {},
	"author":             {},
	"priority":           {},
	"horizon":            {},
	"tags":               {},
	"blocked_by":         {},
	"depends_on":         {},
	"promotion_criteria": {},
	"promoted_to":        {},
	"gathered_from":      {},
	"gathered_at":        {},
	"gathered_into":      {},
	"updated_at":         {},
	"assigned_to":        {},
	"assigned_at":        {},
	"work_ref":           {},
}

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

	if !bytes.HasPrefix(content, frontmatterStart) {
		return nil, ErrInvalidFrontmatter
	}

	rest := content[len(frontmatterStart):]
	idx := bytes.Index(rest, frontmatterDelimiter)
	if idx < 0 {
		return nil, ErrInvalidFrontmatter
	}

	frontmatter := bytes.TrimSpace(rest[:idx])
	body := rest[idx+len(frontmatterDelimiter):]

	// Handle case where content starts with --- but has no second delimiter
	if len(frontmatter) == 0 && len(bytes.TrimSpace(body)) == 0 {
		return nil, ErrInvalidFrontmatter
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(frontmatter, &doc); err != nil {
		return nil, camperrors.Wrapf(ErrFrontmatterParse, "%v", err)
	}

	// Parse YAML into intermediate struct (supports legacy project field).
	var parsed parsedIntent
	if err := doc.Decode(&parsed); err != nil {
		return nil, camperrors.Wrapf(ErrFrontmatterParse, "%v", err)
	}
	parsed.frontmatterExtras = extractUnknownFrontmatter(&doc)

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
	if len(intent.frontmatterExtras) > 0 {
		var doc yaml.Node
		if err := yaml.Unmarshal(frontmatter, &doc); err != nil {
			return nil, camperrors.Wrapf(ErrFrontmatterMarshal, "%v", err)
		}
		if mapping := frontmatterMapping(&doc); mapping != nil {
			appendUnknownFrontmatter(mapping, intent.frontmatterExtras)
			frontmatter, err = yaml.Marshal(&doc)
			if err != nil {
				return nil, camperrors.Wrapf(ErrFrontmatterMarshal, "%v", err)
			}
		}
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

func extractUnknownFrontmatter(doc *yaml.Node) []frontmatterEntry {
	mapping := frontmatterMapping(doc)
	if mapping == nil {
		return nil
	}

	extras := make([]frontmatterEntry, 0)
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		if key == nil {
			continue
		}
		if _, known := knownFrontmatterKeys[key.Value]; known {
			continue
		}
		extras = append(extras, frontmatterEntry{
			Key:   cloneYAMLNode(key),
			Value: cloneYAMLNode(mapping.Content[i+1]),
		})
	}
	return extras
}

func appendUnknownFrontmatter(mapping *yaml.Node, extras []frontmatterEntry) {
	present := make(map[string]struct{}, len(mapping.Content)/2+len(extras))
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i] != nil {
			present[mapping.Content[i].Value] = struct{}{}
		}
	}

	for _, extra := range extras {
		key := extra.Key.Value
		if _, known := knownFrontmatterKeys[key]; known {
			continue
		}
		if _, exists := present[key]; exists {
			continue
		}

		keyNode := cloneYAMLNode(&extra.Key)
		valueNode := cloneYAMLNode(&extra.Value)
		mapping.Content = append(mapping.Content, &keyNode, &valueNode)
		present[key] = struct{}{}
	}
}

func frontmatterMapping(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	return doc
}

func cloneYAMLNode(node *yaml.Node) yaml.Node {
	if node == nil {
		return yaml.Node{}
	}
	clone := *node
	if len(node.Content) > 0 {
		clone.Content = make([]*yaml.Node, len(node.Content))
		for i, child := range node.Content {
			childClone := cloneYAMLNode(child)
			clone.Content[i] = &childClone
		}
	}
	return clone
}
