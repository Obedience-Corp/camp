package workflow

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"text/template"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"gopkg.in/yaml.v3"
)

// Init creates a new workflow with the default structure.
// It creates the schema file, all directories defined in the schema,
// and OBEY.md documentation files in active/, ready/, and dungeon/.
// Returns ErrSchemaExists if a schema already exists and Force is false.
// Returns FlowNestedError if attempting to create a flow inside another flow.
func (s *Service) Init(ctx context.Context, opts InitOptions) (*InitResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &InitResult{
		CreatedDirs:  []string{},
		CreatedFiles: []string{},
		Skipped:      []string{},
	}

	// Check for flow nesting - cannot create a flow inside another flow
	if parentPath, found := HasParentFlow(s.root); found {
		return nil, &FlowNestedError{ParentSchemaPath: parentPath}
	}

	// Check if schema already exists
	if _, err := os.Stat(s.schemaPath); err == nil {
		if !opts.Force {
			return nil, ErrSchemaExists
		}
		result.Skipped = append(result.Skipped, s.schemaPath)
	}

	// Get default schema with optional custom name/description
	var schema *Schema
	if opts.SchemaVersion == 2 {
		schema = DefaultSchemaV2WithInfo(opts.Name, opts.Description)
	} else {
		schema = DefaultSchemaWithInfo(opts.Name, opts.Description)
	}

	// Write schema file
	data, err := yaml.Marshal(schema)
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to marshal schema")
	}

	if err := os.WriteFile(s.schemaPath, data, 0644); err != nil {
		return nil, camperrors.Wrap(err, "failed to write schema file")
	}
	result.CreatedFiles = append(result.CreatedFiles, s.schemaPath)

	// Store schema
	s.schema = schema

	// Create directories
	for _, dirPath := range schema.AllDirectories() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		fullPath := s.resolvePath(dirPath)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			return nil, camperrors.Wrapf(err, "failed to create directory %s", dirPath)
		}
		result.CreatedDirs = append(result.CreatedDirs, dirPath)
	}

	// Create OBEY.md files based on schema version
	var obeyFiles []struct {
		path        string
		getTemplate func() ([]byte, error)
	}
	if schema.Version == 2 {
		obeyFiles = []struct {
			path        string
			getTemplate func() ([]byte, error)
		}{
			{filepath.Join(s.root, "dungeon", "OBEY.md"), GetDungeonOBEYTemplate},
		}
	} else {
		obeyFiles = []struct {
			path        string
			getTemplate func() ([]byte, error)
		}{
			{filepath.Join(s.root, "active", "OBEY.md"), GetActiveOBEYTemplate},
			{filepath.Join(s.root, "ready", "OBEY.md"), GetReadyOBEYTemplate},
			{filepath.Join(s.root, "dungeon", "OBEY.md"), GetDungeonOBEYTemplate},
		}
	}

	for _, obey := range obeyFiles {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Skip if exists and not forcing
		if _, err := os.Stat(obey.path); err == nil {
			if !opts.Force {
				result.Skipped = append(result.Skipped, obey.path)
				continue
			}
		}

		content, err := obey.getTemplate()
		if err != nil {
			return nil, camperrors.Wrapf(err, "failed to read template for %s", obey.path)
		}

		if err := os.WriteFile(obey.path, content, 0644); err != nil {
			return nil, camperrors.Wrapf(err, "failed to write %s", obey.path)
		}
		result.CreatedFiles = append(result.CreatedFiles, obey.path)
	}

	// Create root OBEY.md from Go template
	rootOBEYPath := filepath.Join(s.root, "OBEY.md")
	created, err := s.createRootOBEY(ctx, schema, opts.Force)
	if err != nil {
		return nil, err
	}
	if created {
		result.CreatedFiles = append(result.CreatedFiles, rootOBEYPath)
	} else {
		result.Skipped = append(result.Skipped, rootOBEYPath)
	}

	// Create .gitkeep in empty directories that won't get other files
	emptyDirs := []string{"dungeon/completed", "dungeon/archived", "dungeon/someday"}
	for _, dirPath := range emptyDirs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		gitkeepPath := filepath.Join(s.resolvePath(dirPath), ".gitkeep")
		if _, err := os.Stat(gitkeepPath); os.IsNotExist(err) {
			if err := os.WriteFile(gitkeepPath, []byte{}, 0644); err != nil {
				return nil, camperrors.Wrapf(err, "failed to create .gitkeep in %s", dirPath)
			}
			result.CreatedFiles = append(result.CreatedFiles, filepath.Join(dirPath, ".gitkeep"))
		}
	}

	return result, nil
}

// createRootOBEY renders and writes the root OBEY.md from the Go template.
// Returns true if the file was written, false if skipped.
func (s *Service) createRootOBEY(ctx context.Context, schema *Schema, force bool) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	obeyPath := filepath.Join(s.root, "OBEY.md")

	// Skip if exists and not forcing
	if _, err := os.Stat(obeyPath); err == nil && !force {
		return false, nil
	}

	tmplBytes, err := GetFlowRootOBEYTemplate()
	if err != nil {
		return false, camperrors.Wrap(err, "failed to read root OBEY.md template")
	}

	tmpl, err := template.New("root_obey").Parse(string(tmplBytes))
	if err != nil {
		return false, camperrors.Wrap(err, "failed to parse root OBEY.md template")
	}

	data := struct {
		Name        string
		Description string
	}{
		Name:        schema.Name,
		Description: schema.Description,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return false, camperrors.Wrap(err, "failed to render root OBEY.md")
	}

	if err := os.WriteFile(obeyPath, buf.Bytes(), 0644); err != nil {
		return false, camperrors.Wrap(err, "failed to write root OBEY.md")
	}

	return true, nil
}
