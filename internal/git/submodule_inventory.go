package git

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// InventoryState describes the relationship between one owner's declaration,
// index gitlink, and local child checkout.
type InventoryState string

const (
	InventoryMatched         InventoryState = "matched"
	InventoryDeclarationOnly InventoryState = "declaration_only"
	InventoryGitlinkOnly     InventoryState = "gitlink_only"
	InventoryUninitialized   InventoryState = "uninitialized"
	InventoryInvalidPath     InventoryState = "invalid_path"
	InventoryUnknownMapping  InventoryState = "unknown_mapping"
)

// SubmoduleDeclaration retains a .gitmodules section name separately from its path.
type SubmoduleDeclaration struct {
	Name   string
	Path   string
	URL    string
	Branch string
}

// SubmoduleInventoryEntry is one owner-relative mapping. Path is displayed
// relative to the campaign root; RelativePath belongs to Owner.
type SubmoduleInventoryEntry struct {
	Owner          string
	Path           string
	RelativePath   string
	Name           string
	DeclaredURL    string
	ResolvedURL    string
	Branch         string
	RecordedCommit string
	ActualCommit   string
	Initialized    bool
	State          InventoryState
	Error          error
}

type SubmoduleInventory struct {
	Entries []SubmoduleInventoryEntry
	Errors  []error
}

// SelectedCommitMatches accepts an exact gitlink checkout or an intentional
// branch advance from the recorded commit. It does not move any refs.
func SelectedCommitMatches(ctx context.Context, repoRoot string, entry SubmoduleInventoryEntry) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if entry.RecordedCommit == "" || entry.ActualCommit == "" {
		return false, camperrors.Newf("cannot compare commits for %s: recorded or actual commit is missing", entry.Path)
	}
	if entry.RecordedCommit == entry.ActualCommit {
		return true, nil
	}
	childDir := filepath.Join(repoRoot, entry.Path)
	// merge-base returns exit 1 for both unrelated histories and an absent pin.
	// A stale pin cannot establish ancestry, so expose it as unavailable.
	if err := exec.CommandContext(ctx, "git", "-C", childDir, "cat-file", "-e", entry.RecordedCommit+"^{commit}").Run(); err != nil {
		return false, camperrors.Wrapf(err, "recorded gitlink %s unavailable in %s", entry.RecordedCommit, entry.Path)
	}
	return IsAncestor(ctx, childDir, entry.RecordedCommit, entry.ActualCommit)
}

// InventoryURLMatches compares the owner's active URL with its declaration.
// Git may store a relative declaration as either the raw value or its
// owner-resolved URL, so both forms are accepted.
func InventoryURLMatches(ctx context.Context, repoRoot string, entry SubmoduleInventoryEntry) (bool, string, error) {
	if entry.DeclaredURL == "" {
		return false, "", camperrors.Newf("submodule %s has no declared URL", entry.Path)
	}
	ownerDir := repoRoot
	if entry.Owner != "." {
		ownerDir = filepath.Join(repoRoot, entry.Owner)
	}
	active, err := GetActiveURL(ctx, ownerDir, entry.RelativePath)
	if err != nil {
		return false, "", camperrors.Wrapf(err, "read active URL for %s in %s", entry.Path, entry.Owner)
	}
	return active == entry.DeclaredURL || active == entry.ResolvedURL, active, nil
}

// ListSubmoduleDeclarations parses each .gitmodules section by name. A missing
// file is an empty declaration set; malformed metadata is an error.
func ListSubmoduleDeclarations(ctx context.Context, ownerDir string) ([]SubmoduleDeclaration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(ownerDir, ".gitmodules")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, camperrors.Wrapf(err, "read .gitmodules in %s", ownerDir)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", ownerDir, "config", "-f", ".gitmodules", "--null", "--list")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, camperrors.Wrapf(err, "parse .gitmodules in %s: %s", ownerDir, strings.TrimSpace(string(output)))
	}
	byName := map[string]*SubmoduleDeclaration{}
	for _, record := range bytes.Split(output, []byte{0}) {
		key, value, ok := bytes.Cut(record, []byte{'\n'})
		if !ok || !bytes.HasPrefix(key, []byte("submodule.")) {
			continue
		}
		rest := strings.TrimPrefix(string(key), "submodule.")
		cut := strings.LastIndexByte(rest, '.')
		if cut < 1 {
			continue
		}
		name, property := rest[:cut], rest[cut+1:]
		declaration := byName[name]
		if declaration == nil {
			declaration = &SubmoduleDeclaration{Name: name}
			byName[name] = declaration
		}
		switch property {
		case "path":
			declaration.Path = string(value)
		case "url":
			declaration.URL = string(value)
		case "branch":
			declaration.Branch = string(value)
		}
	}
	declarations := make([]SubmoduleDeclaration, 0, len(byName))
	for _, declaration := range byName {
		declarations = append(declarations, *declaration)
	}
	sort.Slice(declarations, func(i, j int) bool {
		if declarations[i].Path == declarations[j].Path {
			return declarations[i].Name < declarations[j].Name
		}
		return declarations[i].Path < declarations[j].Path
	})
	return declarations, nil
}

// InventorySubmodules visits each initialized repository using its own
// declarations and index gitlinks. It never mutates metadata or checkouts.
func InventorySubmodules(ctx context.Context, repoRoot string) SubmoduleInventory {
	var inventory SubmoduleInventory
	inventory.walk(ctx, repoRoot, ".", map[string]bool{})
	return inventory
}

func (inventory *SubmoduleInventory) walk(ctx context.Context, ownerDir, ownerPath string, visited map[string]bool) {
	if err := ctx.Err(); err != nil {
		inventory.Errors = append(inventory.Errors, camperrors.Wrapf(err, "inventory owner %s", ownerPath))
		return
	}
	initialized, err := IsWorktreeRoot(ctx, ownerDir)
	if err != nil || !initialized {
		if err == nil {
			err = ErrSubmoduleNotInitialized
		}
		inventory.Errors = append(inventory.Errors, camperrors.Wrapf(err, "inventory owner %s", ownerPath))
		return
	}
	actualOwner, err := filepath.EvalSymlinks(ownerDir)
	if err != nil {
		inventory.Errors = append(inventory.Errors, camperrors.Wrapf(err, "resolve owner %s", ownerPath))
		return
	}
	if visited[actualOwner] {
		inventory.Errors = append(inventory.Errors, camperrors.Newf("inventory owner %s: repository cycle at %s", ownerPath, actualOwner))
		return
	}
	visited[actualOwner] = true
	defer delete(visited, actualOwner)

	declarations, declarationErr := ListSubmoduleDeclarations(ctx, ownerDir)
	if declarationErr != nil {
		inventory.Errors = append(inventory.Errors, camperrors.Wrapf(declarationErr, "inventory declarations in %s", ownerPath))
	}
	links, linkErr := listIndexGitlinks(ctx, ownerDir)
	if linkErr != nil {
		inventory.Errors = append(inventory.Errors, camperrors.Wrapf(linkErr, "inventory gitlinks in %s", ownerPath))
		return
	}
	declaredByPath := make(map[string]SubmoduleDeclaration, len(declarations))
	for _, declaration := range declarations {
		if declaration.Path == "" {
			inventory.Errors = append(inventory.Errors, camperrors.Newf("owner %s: .gitmodules section %q has no path", ownerPath, declaration.Name))
			continue
		}
		if previous, exists := declaredByPath[declaration.Path]; exists {
			inventory.Errors = append(inventory.Errors, camperrors.Newf("owner %s: .gitmodules sections %q and %q share path %q", ownerPath, previous.Name, declaration.Name, declaration.Path))
			continue
		}
		declaredByPath[declaration.Path] = declaration
	}
	linksByPath := make(map[string]OrphanedGitlink, len(links))
	for _, link := range links {
		linksByPath[link.Path] = link
	}
	pathSet := map[string]bool{}
	for path := range declaredByPath {
		pathSet[path] = true
	}
	for path := range linksByPath {
		pathSet[path] = true
	}
	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		declaration, declared := declaredByPath[path]
		link, linked := linksByPath[path]
		displayPath := path
		if ownerPath != "." {
			displayPath = filepath.Join(ownerPath, path)
		}
		entry := SubmoduleInventoryEntry{Owner: ownerPath, Path: displayPath, RelativePath: path,
			Name: declaration.Name, DeclaredURL: declaration.URL, Branch: declaration.Branch,
			RecordedCommit: link.Commit}
		switch {
		case declarationErr != nil:
			entry.State = InventoryUnknownMapping
		case declared && linked:
			entry.State = InventoryMatched
		case declared:
			entry.State = InventoryDeclarationOnly
		case linked:
			entry.State = InventoryGitlinkOnly
		}
		if err := pathutil.ValidateSubmodulePath(ownerDir, path); err != nil {
			entry.State = InventoryInvalidPath
			entry.Error = err
			inventory.Entries = append(inventory.Entries, entry)
			inventory.Errors = append(inventory.Errors, camperrors.Wrapf(err, "owner %s submodule path %q", ownerPath, path))
			continue
		}
		if declared && declaration.URL != "" {
			entry.ResolvedURL = resolveSubmoduleURL(ctx, ownerDir, declaration.URL)
		}
		if linked {
			childDir := filepath.Join(ownerDir, path)
			childInitialized, identityErr := IsWorktreeRoot(ctx, childDir)
			if identityErr != nil {
				entry.Error = identityErr
				inventory.Errors = append(inventory.Errors, camperrors.Wrapf(identityErr, "identify %s", displayPath))
			} else if childInitialized {
				entry.Initialized = true
				entry.ActualCommit, entry.Error = Output(ctx, childDir, "rev-parse", "HEAD")
				if entry.Error != nil {
					inventory.Errors = append(inventory.Errors, camperrors.Wrapf(entry.Error, "read HEAD of %s", displayPath))
				}
			} else if declared {
				entry.State = InventoryUninitialized
			}
		}
		inventory.Entries = append(inventory.Entries, entry)
		if linked && entry.Initialized && entry.Error == nil {
			inventory.walk(ctx, filepath.Join(ownerDir, path), displayPath, visited)
		}
	}
}
