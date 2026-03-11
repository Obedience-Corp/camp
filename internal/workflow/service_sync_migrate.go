package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"gopkg.in/yaml.v3"
)

// Sync ensures all directories defined in the schema exist.
// It creates any missing directories but does not remove extra directories.
// If DryRun is true, it reports what would be created without making changes.
func (s *Service) Sync(ctx context.Context, opts SyncOptions) (*SyncResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Load schema if not already loaded
	if s.schema == nil {
		if err := s.LoadSchema(ctx); err != nil {
			return nil, err
		}
	}

	result := &SyncResult{}

	// Check each directory
	for _, dirPath := range s.schema.AllDirectories() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		fullPath := s.resolvePath(dirPath)
		info, err := os.Stat(fullPath)
		if err == nil && info.IsDir() {
			result.Existing = append(result.Existing, dirPath)
			continue
		}

		// Directory doesn't exist
		if !opts.DryRun {
			if err := os.MkdirAll(fullPath, 0755); err != nil {
				return nil, camperrors.Wrapf(err, "failed to create directory %s", dirPath)
			}
		}
		result.Created = append(result.Created, dirPath)
	}

	return result, nil
}

// Migrate upgrades a legacy dungeon structure to a full workflow.
// It creates a .workflow.yaml file and any missing directories.
func (s *Service) Migrate(ctx context.Context, opts MigrateOptions) (*MigrateResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &MigrateResult{}

	// Check for existing dungeon directory
	dungeonPath := s.resolvePath("dungeon")
	if _, err := os.Stat(dungeonPath); os.IsNotExist(err) {
		// No dungeon - just do a regular init
		schema := DefaultSchema()
		initResult, err := s.Init(ctx, InitOptions{Force: opts.Force})
		if err != nil {
			return nil, err
		}
		result.Created = append(result.Created, initResult.CreatedFiles...)
		result.Created = append(result.Created, initResult.CreatedDirs...)
		result.Schema = schema
		return result, nil
	}

	// Dungeon exists - preserve it and add workflow
	result.Preserved = append(result.Preserved, "dungeon/")

	// Check for subdirectories
	entries, err := os.ReadDir(dungeonPath)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				result.Preserved = append(result.Preserved, "dungeon/"+entry.Name()+"/")
			}
		}
	}

	if !opts.DryRun {
		// Create schema and remaining directories
		schema := DefaultSchema()
		data, err := yaml.Marshal(schema)
		if err != nil {
			return nil, camperrors.Wrap(err, "failed to marshal schema")
		}

		if err := os.WriteFile(s.schemaPath, data, 0644); err != nil {
			return nil, camperrors.Wrap(err, "failed to write schema file")
		}
		result.Created = append(result.Created, s.schemaPath)
		s.schema = schema

		// Create non-dungeon directories
		for _, dir := range []string{"active", "ready"} {
			dirPath := s.resolvePath(dir)
			if _, err := os.Stat(dirPath); os.IsNotExist(err) {
				if err := os.MkdirAll(dirPath, 0755); err != nil {
					return nil, camperrors.Wrapf(err, "failed to create directory %s", dir)
				}
				result.Created = append(result.Created, dir+"/")
			}
		}

		// Create any missing dungeon subdirectories
		for childName := range schema.Directories["dungeon"].Children {
			childPath := s.resolvePath("dungeon/" + childName)
			if _, err := os.Stat(childPath); os.IsNotExist(err) {
				if err := os.MkdirAll(childPath, 0755); err != nil {
					return nil, camperrors.Wrapf(err, "failed to create directory dungeon/%s", childName)
				}
				result.Created = append(result.Created, "dungeon/"+childName+"/")
			}
		}

		// Create OBEY.md files if they don't exist
		obeyFiles := []struct {
			path        string
			getTemplate func() ([]byte, error)
		}{
			{filepath.Join(s.root, "active", "OBEY.md"), GetActiveOBEYTemplate},
			{filepath.Join(s.root, "ready", "OBEY.md"), GetReadyOBEYTemplate},
			{filepath.Join(s.root, "dungeon", "OBEY.md"), GetDungeonOBEYTemplate},
		}

		for _, obey := range obeyFiles {
			if _, err := os.Stat(obey.path); os.IsNotExist(err) {
				content, err := obey.getTemplate()
				if err != nil {
					return nil, camperrors.Wrapf(err, "failed to read template for %s", obey.path)
				}
				if err := os.WriteFile(obey.path, content, 0644); err != nil {
					return nil, camperrors.Wrapf(err, "failed to write %s", obey.path)
				}
				result.Created = append(result.Created, obey.path)
			}
		}

		result.Schema = schema
	}

	return result, nil
}

// MigrateV1ToV2 upgrades a v1 workflow to v2 dungeon-centric model.
// Moves active/ and ready/ items to root (both are active work in v2),
// removes empty active/ and ready/ dirs, and updates schema to v2.
func (s *Service) MigrateV1ToV2(ctx context.Context, dryRun bool) (*MigrateV1ToV2Result, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if s.schema == nil {
		if err := s.LoadSchema(ctx); err != nil {
			return nil, err
		}
	}

	if s.schema.Version == 2 {
		return nil, fmt.Errorf("workflow is already v2")
	}

	result := &MigrateV1ToV2Result{}

	// Move active/ and ready/ items to root (both are active work in v2)
	for _, statusDir := range []string{"active", "ready"} {
		dirPath := s.resolvePath(statusDir)
		if entries, err := os.ReadDir(dirPath); err == nil {
			for _, entry := range entries {
				name := entry.Name()
				if name == ".gitkeep" || name == "OBEY.md" {
					continue
				}
				src := filepath.Join(dirPath, name)
				dst := filepath.Join(s.root, name)

				if !dryRun {
					if err := os.Rename(src, dst); err != nil {
						return nil, camperrors.Wrapf(err, "moving %s to root", name)
					}
				}
				result.MovedItems = append(result.MovedItems, fmt.Sprintf("%s/%s → ./%s", statusDir, name, name))
			}
		}
	}

	// Remove empty active/ and ready/ directories
	for _, dir := range []string{s.resolvePath("active"), s.resolvePath("ready")} {
		if entries, err := os.ReadDir(dir); err == nil {
			isEmpty := true
			for _, e := range entries {
				if e.Name() != ".gitkeep" && e.Name() != "OBEY.md" {
					isEmpty = false
					break
				}
			}
			if isEmpty && !dryRun {
				if err := os.RemoveAll(dir); err == nil {
					result.RemovedDirs = append(result.RemovedDirs, dir)
				}
			} else if isEmpty {
				result.RemovedDirs = append(result.RemovedDirs, dir)
			}
		}
	}

	// Update schema to v2
	if !dryRun {
		newSchema := DefaultSchemaV2()
		data, err := yaml.Marshal(newSchema)
		if err != nil {
			return nil, camperrors.Wrap(err, "marshaling v2 schema")
		}
		if err := os.WriteFile(s.schemaPath, data, 0644); err != nil {
			return nil, camperrors.Wrap(err, "writing v2 schema")
		}
		s.schema = newSchema
	}
	result.SchemaUpdate = true

	return result, nil
}
