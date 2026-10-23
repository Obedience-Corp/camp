package skills

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/lancekrogers/guild-scaffold/pkg/scaffold"
)

const shippedSkillsPrefix = ".campaign/skills/"

// ShippedFile is one file of a skill bundle camp ships, rendered for a camp.
type ShippedFile struct {
	// Bundle is the bundle slug, the directory name under .campaign/skills/.
	Bundle string
	// Path is the slash-separated path of the file inside the bundle.
	Path string
	// Content is the rendered file content.
	Content []byte
}

// RelPath returns the camp-root-relative slash path of the file.
func (f ShippedFile) RelPath() string {
	return shippedSkillsPrefix + f.Bundle + "/" + f.Path
}

// Hash returns the hex-encoded sha256 of the rendered content.
func (f ShippedFile) Hash() string {
	return HashContent(f.Content)
}

// HashContent returns the hex-encoded sha256 of content.
func HashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// RenderShipped renders every .campaign/skills/ file of the scaffold recipe at
// recipePath in templateFS with vars, through the same scaffold engine camp
// init uses, and returns them sorted by bundle and path.
func RenderShipped(ctx context.Context, templateFS fs.FS, recipePath string, vars map[string]any) ([]ShippedFile, error) {
	mem := scaffold.NewMemoryFileSystem()
	engine, err := scaffold.NewEngine(templateFS, mem)
	if err != nil {
		return nil, camperrors.Wrap(err, "create skill template engine")
	}
	recipe, err := engine.LoadRecipeFS(ctx, templateFS, recipePath)
	if err != nil {
		return nil, camperrors.Wrap(err, "load skill template recipe")
	}

	skillsRecipe := *recipe
	skillsRecipe.Files = nil
	for _, entry := range recipe.Files {
		if _, _, ok := splitShippedPath(entry.Path); ok {
			skillsRecipe.Files = append(skillsRecipe.Files, entry)
		}
	}
	if _, err := engine.RenderFS(ctx, &skillsRecipe, scaffold.Options{
		TemplatesFS: templateFS,
		Dest:        ".",
		Vars:        vars,
		Overwrite:   true,
	}); err != nil {
		return nil, camperrors.Wrap(err, "render shipped skills")
	}

	files := make([]ShippedFile, 0, len(skillsRecipe.Files))
	for _, entry := range skillsRecipe.Files {
		bundle, rel, _ := splitShippedPath(entry.Path)
		content, err := mem.ReadFile(entry.Path)
		if err != nil {
			return nil, camperrors.Wrapf(err, "read rendered skill %s", entry.Path)
		}
		files = append(files, ShippedFile{Bundle: bundle, Path: rel, Content: content})
	}
	slices.SortFunc(files, func(a, b ShippedFile) int {
		return cmp.Or(strings.Compare(a.Bundle, b.Bundle), strings.Compare(a.Path, b.Path))
	})
	return files, nil
}

// ShippedSlugs returns the sorted, unique bundle slugs in shipped.
func ShippedSlugs(shipped []ShippedFile) []string {
	slugs := make([]string, 0, len(shipped))
	for _, f := range shipped {
		slugs = append(slugs, f.Bundle)
	}
	slices.Sort(slugs)
	return slices.Compact(slugs)
}

func splitShippedPath(p string) (bundle, rel string, ok bool) {
	rest, found := strings.CutPrefix(filepath.ToSlash(p), shippedSkillsPrefix)
	if !found {
		return "", "", false
	}
	bundle, rel, found = strings.Cut(rest, "/")
	if !found || bundle == "" || rel == "" {
		return "", "", false
	}
	return bundle, rel, true
}
