package concept

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// FSService is a concept service backed by an fs.FS.
type FSService struct {
	campaignRoot string
	concepts     []config.ConceptEntry
	fsys         fs.FS
}

// NewFSService creates a concept service with a custom filesystem.
func NewFSService(campaignRoot string, concepts []config.ConceptEntry, fsys fs.FS) *FSService {
	return &FSService{
		campaignRoot: campaignRoot,
		concepts:     concepts,
		fsys:         fsys,
	}
}

func (s *FSService) List(ctx context.Context) ([]Concept, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	var result []Concept
	for _, c := range s.concepts {
		if c.Path == "" && len(c.Children) == 0 {
			continue
		}
		result = append(result, Concept{
			Name:        c.Name,
			Path:        c.Path,
			Description: c.Description,
			HasItems:    len(c.Children) > 0 || s.hasItemsWithIgnore(c.Path, c.Ignore, c.Depth),
			MaxDepth:    c.Depth,
			Ignore:      c.Ignore,
			Children:    childConcepts(c.Children),
		})
	}
	return result, nil
}

func (s *FSService) ListItems(ctx context.Context, conceptName, subpath string) ([]Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	concept, err := s.Get(ctx, conceptName)
	if err != nil {
		return nil, err
	}
	return s.conceptItems(concept, subpath)
}

func (s *FSService) conceptItems(concept *Concept, subpath string) ([]Item, error) {
	if len(concept.Children) > 0 {
		if subpath == "" {
			var disk []Item
			if concept.Path != "" {
				disk, _ = s.listDirItemsFS(concept.Path)
			}
			return parentChildItems(concept, disk, func(rel string) int {
				return s.countChildrenFS(rel)
			}), nil
		}
		if child, rest, ok := childForSubpath(concept, subpath); ok {
			return s.conceptItems(child, rest)
		}
	}

	if concept.Path == "" {
		return []Item{}, nil
	}
	if concept.MaxDepth != nil && *concept.MaxDepth == 0 {
		return []Item{}, nil
	}

	currentDepth := countPathDepth(subpath) + 1
	if concept.MaxDepth != nil && currentDepth > *concept.MaxDepth {
		return []Item{}, nil
	}

	targetPath := concept.Path
	if subpath != "" {
		targetPath = filepath.Join(concept.Path, subpath)
	}

	items, err := s.listDirItemsFS(targetPath)
	if err != nil {
		return nil, err
	}
	if subpath == "" && len(concept.Ignore) > 0 {
		items = filterIgnored(items, concept.Ignore)
	}
	if concept.MaxDepth != nil {
		currentDepth := countPathDepth(subpath)
		if currentDepth+1 >= *concept.MaxDepth {
			for i := range items {
				items[i].DrillDisabled = true
			}
		}
	}
	return items, nil
}

func (s *FSService) Resolve(ctx context.Context, conceptName, item string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", camperrors.Wrap(err, "context cancelled")
	}

	concept, err := s.Get(ctx, conceptName)
	if err != nil {
		return "", err
	}
	if item == "" {
		return filepath.Join(s.campaignRoot, concept.Path), nil
	}
	return filepath.Join(s.campaignRoot, concept.Path, item), nil
}

func (s *FSService) ResolvePath(ctx context.Context, path string) (*Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}
	if path == "" {
		return nil, errors.New("empty path")
	}

	path = filepath.Clean(path)
	info, err := fs.Stat(s.fsys, path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, camperrors.NewNotFound("path", path, nil)
		}
		return nil, camperrors.Wrap(err, "checking path")
	}

	item := &Item{
		Name:  filepath.Base(path),
		Path:  filepath.Join(s.campaignRoot, path),
		IsDir: info.IsDir(),
	}
	if info.IsDir() {
		item.Children = s.countChildrenFS(path)
	}
	return item, nil
}

func (s *FSService) ConceptForPath(ctx context.Context, path string) (*Concept, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	concepts, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	path = filepath.Clean(path)
	for _, c := range concepts {
		conceptPath := filepath.Clean(c.Path)
		if path == conceptPath || hasPathPrefix(path, conceptPath) {
			return &c, nil
		}
	}
	return nil, camperrors.NewNotFound("concept path", path, nil)
}

func (s *FSService) Get(ctx context.Context, name string) (*Concept, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	for _, c := range s.concepts {
		if c.Name == name {
			return &Concept{
				Name:        c.Name,
				Path:        c.Path,
				Description: c.Description,
				HasItems:    len(c.Children) > 0 || s.hasItemsWithIgnore(c.Path, c.Ignore, c.Depth),
				MaxDepth:    c.Depth,
				Ignore:      c.Ignore,
				Children:    childConcepts(c.Children),
			}, nil
		}
	}
	return nil, camperrors.Wrap(ErrNotFound, name)
}

func (s *FSService) hasItemsWithIgnore(path string, ignore []string, depth *int) bool {
	if path == "" {
		return false
	}
	if depth != nil && *depth == 0 {
		return false
	}

	entries, err := fs.ReadDir(s.fsys, path)
	if err != nil {
		return false
	}
	ignoreSet := makeIgnoreSet(ignore)
	for _, entry := range entries {
		if entry.IsDir() && !isHidden(entry.Name()) && !ignoreSet[entry.Name()+"/"] && !ignoreSet[entry.Name()] {
			return true
		}
	}
	return false
}

func (s *FSService) listDirItemsFS(path string) ([]Item, error) {
	entries, err := fs.ReadDir(s.fsys, path)
	if err != nil {
		return []Item{}, nil
	}

	var items []Item
	for _, entry := range entries {
		if isHidden(entry.Name()) {
			continue
		}
		item := Item{
			Name:  entry.Name(),
			Path:  filepath.Join(path, entry.Name()),
			IsDir: entry.IsDir(),
		}
		if entry.IsDir() {
			item.Children = s.countChildrenFS(filepath.Join(path, entry.Name()))
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (s *FSService) countChildrenFS(path string) int {
	entries, err := fs.ReadDir(s.fsys, path)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !isHidden(entry.Name()) {
			count++
		}
	}
	return count
}

var _ Service = (*FSService)(nil)
