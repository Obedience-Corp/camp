package workitem

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
)

// frontmatterMaxHead bounds the head-read for a document workitem's identity
// block. Generous for a real frontmatter block while keeping the byte-zero
// check cheap for the many markdown files that are not workitems.
const frontmatterMaxHead = 4096

// readFrontmatterBlock returns the raw YAML bytes between the opening and
// closing "---" delimiters at the head of path, or (nil, false) if path does
// not begin with "---" at byte zero or has no closing delimiter within maxHead
// bytes. It never reads the full file when the leading delimiter is absent.
func readFrontmatterBlock(path string, maxHead int) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()

	head := make([]byte, maxHead)
	n, _ := io.ReadFull(f, head)
	return parseFrontmatterHead(head[:n])
}

// parseFrontmatterHead extracts the YAML bytes between the opening and closing
// "---" delimiters of a document head, or (nil, false) if head does not begin
// with "---" at byte zero (LF or CRLF) or has no closing delimiter within it.
// Split out from readFrontmatterBlock so the delimiter logic is unit-tested
// without touching the filesystem.
func parseFrontmatterHead(head []byte) ([]byte, bool) {
	var openLen int
	switch {
	case bytes.HasPrefix(head, []byte("---\n")):
		openLen = len("---\n")
	case bytes.HasPrefix(head, []byte("---\r\n")):
		openLen = len("---\r\n")
	default:
		return nil, false
	}
	body := head[openLen:]
	idx := bytes.Index(body, []byte("\n---"))
	if idx < 0 {
		return nil, false
	}
	return body[:idx], true
}

// LoadFrontmatterMetadata reads and validates the kind:workitem frontmatter
// block of the markdown file at path, returning (nil, nil) if the file has no
// such block (mirrors LoadMetadata's "not found" contract for .workitem). The
// gather (07_file_lifecycle_verbs) and links-migration doctor passes reuse this
// to load a file source's Metadata without going through the directory-only
// LoadMetadata.
func LoadFrontmatterMetadata(path string) (*Metadata, error) {
	block, ok := readFrontmatterBlock(path, frontmatterMaxHead)
	if !ok || !bytes.Contains(block, []byte("kind: workitem")) {
		return nil, nil
	}
	var md Metadata
	if err := yaml.Unmarshal(block, &md); err != nil {
		return nil, camperrors.Wrapf(err, "parsing frontmatter %s", path)
	}
	if err := validateMetadata(&md); err != nil {
		return nil, camperrors.Wrapf(err, "validating frontmatter %s", path)
	}
	return &md, nil
}

// discoverWorkflowFrontmatterDocs scans workflow/** for markdown files whose
// frontmatter declares kind: workitem and returns them as file workitems. The
// dungeon and dotfile skips mirror the directory scanner so both detection
// surfaces agree (doc 03). Malformed frontmatter is logged and skipped, not
// fatal, matching buildWorkflowDirItem's log-and-skip on marker parse errors.
func discoverWorkflowFrontmatterDocs(ctx context.Context, campaignRoot string, resolver *paths.Resolver) ([]WorkItem, error) {
	root := resolver.Workflow()
	var items []WorkItem
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == root {
				return filepath.SkipAll
			}
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		name := d.Name()
		if d.IsDir() {
			if name == "dungeon" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".md") {
			return nil
		}
		// Collision rule (doc 03): a directory .workitem owns the identity of
		// everything under it, so a frontmatter file below a marker-bearing
		// directory is never a second workitem. Cheaper than the head-check, so
		// it runs first.
		if hasMarkerAncestor(root, filepath.Dir(path)) {
			return nil
		}
		item, ok := buildFrontmatterItem(ctx, campaignRoot, path, resolver)
		if ok {
			items = append(items, item)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return items, nil
}

// buildFrontmatterItem constructs a file WorkItem from a markdown file's
// kind:workitem frontmatter. It returns (item, false) when the file has no such
// block or its frontmatter is invalid (logged and skipped, never fatal).
func buildFrontmatterItem(ctx context.Context, campaignRoot, path string, resolver *paths.Resolver) (WorkItem, bool) {
	md, err := LoadFrontmatterMetadata(path)
	if err != nil {
		slog.Default().Debug("workitem discovery skip",
			"path", path, "reason", "frontmatter-invalid", "error", err.Error())
		return WorkItem{}, false
	}
	if md == nil {
		return WorkItem{}, false
	}

	relPath, err := filepath.Rel(campaignRoot, path)
	if err != nil {
		return WorkItem{}, false
	}

	// Type authority (doc 03): under a path-typed tree the path decides the
	// type; the frontmatter's own type: is validated against it but never
	// overrides it (mirrors buildWorkflowDirItem). Elsewhere frontmatter is
	// authoritative.
	wfType := WorkflowType(md.Type)
	if forced, ok := frontmatterPathType(resolver, path); ok {
		wfType = forced
	}

	created, updated := ScanFileTimestamps(ctx, path)
	titleStem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	item := WorkItem{
		Key:            "file:" + relPath,
		WorkflowType:   wfType,
		LifecycleStage: LifecycleStageActive,
		Title:          titleFromDoc(path, titleStem),
		RelativePath:   relPath,
		PrimaryDoc:     relPath,
		ItemKind:       ItemKindFile,
		CreatedAt:      created,
		UpdatedAt:      updated,
		Tags:           []string{},
		Projects:       []string{},
	}
	item.SortTimestamp = DeriveSortTimestamp(item.UpdatedAt, item.CreatedAt)
	item.Summary = extractSummaryFromFile(path, 200)

	merged, err := ApplyMetadata(item, md)
	if err != nil {
		slog.Default().Debug("workitem discovery skip",
			"path", path, "reason", "apply-metadata", "error", err.Error())
		return WorkItem{}, false
	}
	return merged, true
}

// frontmatterPathType returns the workflow type a path-typed tree (design or
// explore) forces on a document under it, and true; ("", false) when the path
// has no type convention and the frontmatter's own type: is authoritative.
func frontmatterPathType(resolver *paths.Resolver, path string) (WorkflowType, bool) {
	switch {
	case underRoot(path, resolver.Design()):
		return WorkflowTypeDesign, true
	case underRoot(path, resolver.Explore()):
		return WorkflowTypeExplore, true
	default:
		return "", false
	}
}

func underRoot(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

// hasMarkerAncestor reports whether fileDir or any ancestor up to workflowRoot
// (inclusive) carries a .workitem marker. Such a directory owns the identity of
// everything beneath it, so a frontmatter file below it is never a second
// workitem (doc 03 collision rule).
func hasMarkerAncestor(workflowRoot, fileDir string) bool {
	dir := fileDir
	for {
		if _, err := os.Stat(filepath.Join(dir, MetadataFilename)); err == nil {
			return true
		}
		if dir == workflowRoot || len(dir) <= len(workflowRoot) {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// FrontmatterConflictKind distinguishes a hard identity conflict from a
// recoverable pending-ref state between a directory marker and a document
// frontmatter block that share the same id.
type FrontmatterConflictKind int

const (
	// FrontmatterConflictIdentity is a hard conflict: the frontmatter declares a
	// different id, or the same id with a different ref (both refs set).
	FrontmatterConflictIdentity FrontmatterConflictKind = iota
	// FrontmatterConflictRefPending is a warning: same id, but exactly one side
	// declares a ref. Doctor's ref backfill is frontmatter-unaware and would
	// later mint an unrelated dir ref, flipping this into a hard conflict, so it
	// is surfaced for proactive reconciliation.
	FrontmatterConflictRefPending
)

// FrontmatterConflict describes a document frontmatter block whose identity
// disagrees with, or is only partially reconciled against, the directory
// .workitem that owns its tree.
type FrontmatterConflict struct {
	RelPathFromDir string
	ID             string
	Ref            string
	Kind           FrontmatterConflictKind
}

// FindFrontmatterConflicts walks dirAbs for markdown files whose kind:workitem
// frontmatter identity disagrees with dirMeta (the directory's own .workitem). A
// nested directory carrying its own .workitem owns its subtree and is skipped;
// invalid or absent frontmatter is not a conflict. Validate uses this to enforce
// the identity-conflict rule (doc 03).
func FindFrontmatterConflicts(dirAbs string, dirMeta *Metadata) ([]FrontmatterConflict, error) {
	var conflicts []FrontmatterConflict
	err := filepath.WalkDir(dirAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == dirAbs {
				return nil
			}
			name := d.Name()
			if name == "dungeon" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if _, statErr := os.Stat(filepath.Join(path, MetadataFilename)); statErr == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		md, ferr := LoadFrontmatterMetadata(path)
		if ferr != nil || md == nil {
			return nil
		}
		kind, isFinding := classifyFrontmatterIdentity(md, dirMeta)
		if !isFinding {
			return nil
		}
		rel, relErr := filepath.Rel(dirAbs, path)
		if relErr != nil {
			return nil
		}
		conflicts = append(conflicts, FrontmatterConflict{
			RelPathFromDir: filepath.ToSlash(rel),
			ID:             md.ID,
			Ref:            md.Ref,
			Kind:           kind,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return conflicts, nil
}

// classifyFrontmatterIdentity compares a document frontmatter block against the
// directory .workitem that owns its tree. It reports a hard identity conflict
// when the id differs or both refs are set and differ, a pending-ref warning
// when the ids match but exactly one side declares a ref, and no finding when
// they agree or neither declares a ref.
func classifyFrontmatterIdentity(md, dirMeta *Metadata) (FrontmatterConflictKind, bool) {
	if md.ID != dirMeta.ID {
		return FrontmatterConflictIdentity, true
	}
	dirHasRef := dirMeta.Ref != ""
	fmHasRef := md.Ref != ""
	switch {
	case dirHasRef && fmHasRef && md.Ref != dirMeta.Ref:
		return FrontmatterConflictIdentity, true
	case dirHasRef != fmHasRef:
		return FrontmatterConflictRefPending, true
	default:
		return FrontmatterConflictIdentity, false
	}
}
