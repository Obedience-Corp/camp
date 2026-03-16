package intent

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intentaudit "github.com/Obedience-Corp/camp/internal/intent/audit"
)

var legacyIntentScaffoldFiles = []string{
	"OBEY.md",
	filepath.Join(string(StatusInbox), ".gitkeep"),
	filepath.Join(string(StatusReady), ".gitkeep"),
	filepath.Join(string(StatusActive), ".gitkeep"),
	filepath.Join("dungeon", ".gitkeep"),
	filepath.Join("dungeon", ".crawl.yaml"),
}

// PlannedPathMove describes a filesystem move that migration would perform.
type PlannedPathMove struct {
	Source string
	Dest   string
}

// EnsureDirectories creates all status directories if missing and migrates
// legacy top-level done/ and killed/ directories into the dungeon.
func (s *IntentService) EnsureDirectories(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled")
	}

	if err := s.ensureCanonicalIntentRoot(ctx); err != nil {
		return camperrors.Wrap(err, "ensuring canonical intent root")
	}

	for _, status := range AllStatuses() {
		dir := filepath.Join(s.intentsDir, string(status))
		if err := os.MkdirAll(dir, 0755); err != nil {
			return camperrors.Wrapf(err, "creating directory %s", dir)
		}
	}

	legacyMappings := map[string]Status{
		"done":   StatusDone,
		"killed": StatusKilled,
	}

	for legacyDir, newStatus := range legacyMappings {
		if err := s.migrateLegacyDir(ctx, legacyDir, newStatus); err != nil {
			return camperrors.Wrapf(err, "migrating %s", legacyDir)
		}
	}

	return nil
}

func (s *IntentService) ensureCanonicalIntentRoot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled")
	}

	legacyRoot := s.legacyIntentsDir()
	if filepath.Clean(legacyRoot) == filepath.Clean(s.intentsDir) {
		return nil
	}

	canonicalHasState, err := hasIntentState(s.intentsDir)
	if err != nil {
		return camperrors.Wrapf(err, "inspecting canonical intent root %s", s.intentsDir)
	}

	legacyHasState, err := hasIntentState(legacyRoot)
	if err != nil {
		return camperrors.Wrapf(err, "inspecting legacy intent root %s", legacyRoot)
	}

	if legacyHasState && canonicalHasState {
		return camperrors.Wrapf(
			ErrIntentMigrationConflict,
			"both %s and %s contain intent state; resolve with repair before retrying",
			legacyRoot,
			s.intentsDir,
		)
	}

	if !legacyHasState {
		return nil
	}

	if err := s.migrateLegacyIntentRoot(legacyRoot); err != nil {
		return camperrors.Wrapf(err, "migrating legacy intent root %s", legacyRoot)
	}

	return nil
}

func (s *IntentService) legacyIntentsDir() string {
	return filepath.Join(s.campaignRoot, "workflow", "intents")
}

// PlanLegacyIntentRootMigration returns the filesystem moves required to migrate
// legacy workflow/intents state into the canonical intent root.
func (s *IntentService) PlanLegacyIntentRootMigration() ([]PlannedPathMove, error) {
	legacyRoot := s.legacyIntentsDir()
	if filepath.Clean(legacyRoot) == filepath.Clean(s.intentsDir) {
		return nil, nil
	}

	canonicalHasState, err := hasIntentState(s.intentsDir)
	if err != nil {
		return nil, camperrors.Wrapf(err, "inspecting canonical intent root %s", s.intentsDir)
	}

	legacyHasState, err := hasIntentState(legacyRoot)
	if err != nil {
		return nil, camperrors.Wrapf(err, "inspecting legacy intent root %s", legacyRoot)
	}

	if legacyHasState && canonicalHasState {
		return nil, camperrors.Wrapf(
			ErrIntentMigrationConflict,
			"both %s and %s contain intent state; resolve with repair before retrying",
			legacyRoot,
			s.intentsDir,
		)
	}

	if !legacyHasState {
		return nil, nil
	}

	mappings := [][2]string{
		{filepath.Join(legacyRoot, string(StatusInbox)), filepath.Join(s.intentsDir, string(StatusInbox))},
		{filepath.Join(legacyRoot, string(StatusReady)), filepath.Join(s.intentsDir, string(StatusReady))},
		{filepath.Join(legacyRoot, string(StatusActive)), filepath.Join(s.intentsDir, string(StatusActive))},
		{filepath.Join(legacyRoot, "dungeon"), filepath.Join(s.intentsDir, "dungeon")},
		{filepath.Join(legacyRoot, "done"), filepath.Join(s.intentsDir, "dungeon", string(StatusDone))},
		{filepath.Join(legacyRoot, "killed"), filepath.Join(s.intentsDir, "dungeon", string(StatusKilled))},
	}

	var moves []PlannedPathMove
	for _, mapping := range mappings {
		if err := collectIntentTreeMoves(mapping[0], mapping[1], &moves); err != nil {
			return nil, err
		}
	}

	if err := collectIntentAuditMove(legacyRoot, s.intentsDir, &moves); err != nil {
		return nil, err
	}

	return moves, nil
}

// PlanLegacyIntentRootCleanup returns scaffold-generated legacy intent paths
// that can be removed after normalization to the canonical root.
func (s *IntentService) PlanLegacyIntentRootCleanup() ([]string, error) {
	legacyRoot := s.legacyIntentsDir()
	if filepath.Clean(legacyRoot) == filepath.Clean(s.intentsDir) {
		return nil, nil
	}

	return collectLegacyIntentScaffoldFiles(legacyRoot)
}

// CleanupLegacyIntentScaffold removes obsolete workflow/intents scaffold files.
// This is intended for explicit init/repair normalization, not routine commands.
func (s *IntentService) CleanupLegacyIntentScaffold() error {
	legacyRoot := s.legacyIntentsDir()
	if filepath.Clean(legacyRoot) == filepath.Clean(s.intentsDir) {
		return nil
	}

	return cleanupLegacyIntentScaffold(legacyRoot)
}

func hasIntentState(root string) (bool, error) {
	if info, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, camperrors.Wrapf(err, "stat %s", root)
	} else if !info.IsDir() {
		return false, camperrors.Wrapf(ErrInvalidPath, "%s is not a directory", root)
	}

	hasAudit, err := hasNonEmptyFile(intentaudit.FilePath(root))
	if err != nil {
		return false, err
	}
	if hasAudit {
		return true, nil
	}

	stateDirs := []string{
		string(StatusInbox),
		string(StatusReady),
		string(StatusActive),
		string(StatusDone),
		string(StatusKilled),
		string(StatusArchived),
		string(StatusSomeday),
		"done",
		"killed",
	}

	for _, relDir := range stateDirs {
		hasMarkdown, err := hasMarkdownFiles(filepath.Join(root, relDir))
		if err != nil {
			return false, err
		}
		if hasMarkdown {
			return true, nil
		}
	}

	return false, nil
}

func hasNonEmptyFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, camperrors.Wrapf(err, "stat %s", path)
	}
	if info.IsDir() {
		return false, camperrors.Wrapf(ErrInvalidPath, "%s is not a file", path)
	}
	return info.Size() > 0, nil
}

func hasMarkdownFiles(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, camperrors.Wrapf(err, "reading directory %s", dir)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".md") {
			return true, nil
		}
	}

	return false, nil
}

func (s *IntentService) migrateLegacyIntentRoot(legacyRoot string) error {
	for _, relDir := range []string{
		string(StatusInbox),
		string(StatusReady),
		string(StatusActive),
		"dungeon",
		"done",
		"killed",
	} {
		if err := moveIntentTree(filepath.Join(legacyRoot, relDir), filepath.Join(s.intentsDir, relDir)); err != nil {
			return err
		}
	}

	if err := moveIntentAuditFile(legacyRoot, s.intentsDir); err != nil {
		return err
	}

	return nil
}

// migrateLegacyDir moves intent files from a legacy top-level status directory
// into the corresponding dungeon subdirectory, updating frontmatter status.
func (s *IntentService) migrateLegacyDir(ctx context.Context, legacyDir string, newStatus Status) error {
	srcDir := filepath.Join(s.intentsDir, legacyDir)

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrapf(err, "reading directory %s", srcDir)
	}

	dstDir := filepath.Join(s.intentsDir, string(newStatus))

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return camperrors.Wrap(err, "context cancelled")
		}

		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())

		content, err := os.ReadFile(srcPath)
		if err != nil {
			return camperrors.Wrapf(err, "reading %s", srcPath)
		}

		intent, err := ParseIntentFromFile(srcPath, content)
		if err != nil {
			if _, serr := os.Stat(dstPath); serr == nil {
				_ = os.Remove(srcPath)
				continue
			}
			if err := os.Rename(srcPath, dstPath); err != nil {
				return camperrors.Wrapf(err, "moving %s", srcPath)
			}
			continue
		}

		intent.Status = newStatus
		data, err := SerializeIntent(intent)
		if err != nil {
			return camperrors.Wrapf(err, "serializing %s", srcPath)
		}

		if _, serr := os.Stat(dstPath); serr == nil {
			_ = os.Remove(srcPath)
			continue
		}

		if err := os.WriteFile(dstPath, data, 0644); err != nil {
			return camperrors.Wrapf(err, "writing %s", dstPath)
		}

		if err := os.Remove(srcPath); err != nil {
			return camperrors.Wrapf(err, "removing %s", srcPath)
		}
	}

	if remaining, err := os.ReadDir(srcDir); err == nil && len(remaining) == 0 {
		_ = os.Remove(srcDir)
	}

	return nil
}
