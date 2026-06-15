package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	dungeonscaffold "github.com/Obedience-Corp/camp/internal/dungeon/scaffold"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"gopkg.in/yaml.v3"
)

type v1ToV2Move struct {
	statusDir string
	name      string
	src       string
	dst       string
}

type workflowMigrationMover func(src, dst string) error

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

		if err := fsutil.WriteFileAtomically(s.schemaPath, data, 0644); err != nil {
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

		// Create OBEY.md files if they don't exist
		obeyFiles := []struct {
			path        string
			getTemplate func() ([]byte, error)
		}{
			{filepath.Join(s.root, "active", "OBEY.md"), GetActiveOBEYTemplate},
			{filepath.Join(s.root, "ready", "OBEY.md"), GetReadyOBEYTemplate},
		}

		for _, obey := range obeyFiles {
			if _, err := os.Stat(obey.path); os.IsNotExist(err) {
				content, err := obey.getTemplate()
				if err != nil {
					return nil, camperrors.Wrapf(err, "failed to read template for %s", obey.path)
				}
				if err := fsutil.WriteFileAtomically(obey.path, content, 0644); err != nil {
					return nil, camperrors.Wrapf(err, "failed to write %s", obey.path)
				}
				result.Created = append(result.Created, obey.path)
			}
		}

		dungeonResult, err := dungeonscaffold.Init(ctx, s.resolvePath("dungeon"), dungeonscaffold.InitOptions{})
		if err != nil {
			return nil, camperrors.Wrap(err, "failed to initialize dungeon")
		}
		appendStandardDungeonMigrationResult(result, s.root, dungeonResult)

		result.Schema = schema
	}

	return result, nil
}

// MigrateV1ToV2 upgrades a v1 workflow to v2 dungeon-centric model.
// Moves active/ and ready/ items to root (both are active work in v2),
// removes empty active/ and ready/ dirs, and updates schema to v2.
func (s *Service) MigrateV1ToV2(ctx context.Context, dryRun bool) (*MigrateV1ToV2Result, error) {
	return s.migrateV1ToV2(ctx, dryRun, migrateWorkflowItemNoReplace)
}

func (s *Service) migrateV1ToV2(ctx context.Context, dryRun bool, moveItem workflowMigrationMover) (*MigrateV1ToV2Result, error) {
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

	moves, err := s.planV1ToV2Moves()
	if err != nil {
		return nil, err
	}
	if err := validateV1ToV2MovePlan(s.root, moves); err != nil {
		return nil, err
	}

	if !dryRun {
		if err := executeV1ToV2Moves(moves, moveItem); err != nil {
			return nil, err
		}
	}
	for _, move := range moves {
		result.MovedItems = append(result.MovedItems,
			fmt.Sprintf("%s/%s → ./%s", move.statusDir, move.name, move.name))
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
		if err := fsutil.WriteFileAtomically(s.schemaPath, data, 0644); err != nil {
			return nil, camperrors.Wrap(err, "writing v2 schema")
		}
		s.schema = newSchema
	}
	result.SchemaUpdate = true

	return result, nil
}

func (s *Service) planV1ToV2Moves() ([]v1ToV2Move, error) {
	var moves []v1ToV2Move
	for _, statusDir := range []string{"active", "ready"} {
		dirPath := s.resolvePath(statusDir)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, camperrors.Wrapf(err, "reading %s", statusDir)
		}

		for _, entry := range entries {
			name := entry.Name()
			if name == ".gitkeep" || name == "OBEY.md" {
				continue
			}
			moves = append(moves, v1ToV2Move{
				statusDir: statusDir,
				name:      name,
				src:       filepath.Join(dirPath, name),
				dst:       filepath.Join(s.root, name),
			})
		}
	}
	return moves, nil
}

func validateV1ToV2MovePlan(root string, moves []v1ToV2Move) error {
	var errs []string
	seen := make(map[string]v1ToV2Move, len(moves))
	for _, move := range moves {
		if err := checkMigrateDestination(root, move.name); err != nil {
			errs = append(errs, fmt.Sprintf("%s/%s: %v", move.statusDir, move.name, err))
		}
		if previous, ok := seen[move.name]; ok {
			errs = append(errs, fmt.Sprintf(
				"%s/%s: duplicate destination ./%s already planned by %s/%s",
				move.statusDir, move.name, move.name, previous.statusDir, previous.name))
			continue
		}
		seen[move.name] = move
	}
	if len(errs) > 0 {
		return camperrors.New("v1 to v2 migration validation failed:\n  " + strings.Join(errs, "\n  "))
	}
	return nil
}

// checkMigrateDestination enforces no-replace semantics for v1->v2 root moves.
// Structured as a small helper for D003 extraction in sequence 11.05.
func checkMigrateDestination(root, name string) error {
	dst := filepath.Join(root, name)
	if existing, exists, err := resolveWorkflowItemPath(root, ".", name); err != nil {
		return camperrors.Wrapf(err, "checking destination: %s", dst)
	} else if exists {
		return camperrors.Wrapf(ErrAlreadyExists, "destination already exists: %s", existing)
	}
	return nil
}

func executeV1ToV2Moves(moves []v1ToV2Move, moveItem workflowMigrationMover) error {
	moved := make([]v1ToV2Move, 0, len(moves))
	for _, move := range moves {
		if err := moveItem(move.src, move.dst); err != nil {
			if rollbackErr := rollbackV1ToV2Moves(moved); rollbackErr != nil {
				return camperrors.Wrapf(rollbackErr,
					"moving %s/%s to root failed after %d move(s): %v",
					move.statusDir, move.name, len(moved), err)
			}
			return camperrors.Wrapf(err,
				"moving %s/%s to root failed after %d move(s); rolled back",
				move.statusDir, move.name, len(moved))
		}
		moved = append(moved, move)
	}
	return nil
}

func rollbackV1ToV2Moves(moved []v1ToV2Move) error {
	var errs []string
	for i := len(moved) - 1; i >= 0; i-- {
		move := moved[i]
		if err := os.Rename(move.dst, move.src); err != nil {
			errs = append(errs, fmt.Sprintf("%s -> %s: %v", move.dst, move.src, err))
		}
	}
	if len(errs) > 0 {
		return camperrors.New("rollback failed:\n  " + strings.Join(errs, "\n  "))
	}
	return nil
}

// migrateWorkflowItemNoReplace moves one workflow item while refusing to replace
// an existing destination. The full plan is validated before this helper runs.
func migrateWorkflowItemNoReplace(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return camperrors.Wrapf(ErrAlreadyExists, "destination already exists: %s", dst)
	} else if err != nil && !os.IsNotExist(err) {
		return camperrors.Wrapf(err, "checking destination: %s", dst)
	}
	return os.Rename(src, dst)
}
