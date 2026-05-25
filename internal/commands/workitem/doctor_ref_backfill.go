package workitem

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/paths"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// workitemPathsMissingRef discovers every workitem on disk and returns the
// campaign-relative paths to those whose .workitem marker has no ref field.
// Paths are sorted lexicographically so DeriveUnique's collision retry has
// deterministic input ordering during a doctor --fix pass.
func workitemPathsMissingRef(ctx context.Context, root string) ([]string, error) {
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return nil, camperrors.Wrap(err, "load campaign config")
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := wkitem.Discover(ctx, root, resolver)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, item := range items {
		ref, _ := item.SourceMetadata["ref"].(string)
		if ref != "" {
			continue
		}
		missing = append(missing, item.RelativePath)
	}
	sort.Strings(missing)
	return missing, nil
}

// backfillRef rewrites the .workitem at relPath to include a derived ref
// field. All other fields and YAML key order are preserved by mutating only
// the parsed mapping in place.
func backfillRef(ctx context.Context, root, relPath string) error {
	existingRefs, err := refsExcludingPath(ctx, root, relPath)
	if err != nil {
		return err
	}
	id, err := workitemIDAtPath(root, relPath)
	if err != nil {
		return err
	}
	ref, err := wkitem.DeriveUnique(ctx, id, existingRefs)
	if err != nil {
		return err
	}
	return backfillRefWithRef(ctx, root, relPath, ref)
}

func backfillMissingRefs(ctx context.Context, root string) (int, error) {
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return 0, camperrors.Wrap(err, "load campaign config")
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := wkitem.Discover(ctx, root, resolver)
	if err != nil {
		return 0, err
	}

	existingRefs := make(map[string]bool, len(items))
	var pending []wkitem.WorkItem
	for _, item := range items {
		ref, _ := item.SourceMetadata["ref"].(string)
		if ref != "" {
			existingRefs[ref] = true
			continue
		}
		pending = append(pending, item)
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].RelativePath < pending[j].RelativePath
	})

	applied := 0
	for _, item := range pending {
		if err := ctx.Err(); err != nil {
			return applied, err
		}
		ref, err := wkitem.DeriveUnique(ctx, item.StableID, existingRefs)
		if err != nil {
			return applied, err
		}
		existingRefs[ref] = true
		if err := backfillRefWithRef(ctx, root, item.RelativePath, ref); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func backfillRefWithRef(ctx context.Context, root, relPath, ref string) error {
	abs := filepath.Join(root, filepath.FromSlash(relPath), wkitem.MetadataFilename)

	release, err := fsutil.AcquireFileLock(ctx, abs+".lock")
	if err != nil {
		return err
	}
	defer release()

	raw, err := os.ReadFile(abs)
	if err != nil {
		return camperrors.Wrapf(err, "read %s", abs)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return camperrors.Wrapf(err, "parse %s", abs)
	}

	if _, ok := lookupScalar(&doc, "id"); !ok {
		return camperrors.NewValidation("id", "missing id in "+abs, nil)
	}
	if existing, ok := lookupScalar(&doc, "ref"); ok && existing != "" {
		// Another concurrent process already filled it in. No-op.
		return nil
	}

	if err := insertScalarAfter(&doc, "id", "ref", ref); err != nil {
		return err
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return camperrors.Wrap(err, "marshal updated workitem")
	}
	return fsutil.WriteFileAtomically(abs, out, 0o644)
}

func workitemIDAtPath(root, relPath string) (string, error) {
	abs := filepath.Join(root, filepath.FromSlash(relPath), wkitem.MetadataFilename)
	raw, err := os.ReadFile(abs)
	if err != nil {
		return "", camperrors.Wrapf(err, "read %s", abs)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return "", camperrors.Wrapf(err, "parse %s", abs)
	}
	id, ok := lookupScalar(&doc, "id")
	if !ok {
		return "", camperrors.NewValidation("id", "missing id in "+abs, nil)
	}
	return id, nil
}

func refsExcludingPath(ctx context.Context, root, skipRel string) (map[string]bool, error) {
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return nil, camperrors.Wrap(err, "load campaign config")
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := wkitem.Discover(ctx, root, resolver)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(items))
	for _, item := range items {
		if item.RelativePath == skipRel {
			continue
		}
		ref, _ := item.SourceMetadata["ref"].(string)
		if ref != "" {
			out[ref] = true
		}
	}
	return out, nil
}

// lookupScalar finds the scalar value of `key` in a top-level mapping
// document. Returns (value, true) on hit; ("", false) when the document is
// not a mapping or the key is absent.
func lookupScalar(doc *yaml.Node, key string) (string, bool) {
	root := mappingRoot(doc)
	if root == nil {
		return "", false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		v := root.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key && v.Kind == yaml.ScalarNode {
			return v.Value, true
		}
	}
	return "", false
}

// insertScalarAfter inserts {key: value} immediately after the mapping pair
// whose key equals `after`. If `after` is missing, the new pair is appended
// at the end. This keeps the on-disk diff minimal for backfills.
func insertScalarAfter(doc *yaml.Node, after, key, value string) error {
	root := mappingRoot(doc)
	if root == nil {
		return camperrors.NewValidation("doc", "top-level YAML is not a mapping", nil)
	}
	insertAt := len(root.Content)
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		v := root.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			v.Kind = yaml.ScalarNode
			v.Tag = "!!str"
			v.Value = value
			return nil
		}
		if k.Kind == yaml.ScalarNode && k.Value == after {
			insertAt = i + 2
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}

	updated := make([]*yaml.Node, 0, len(root.Content)+2)
	updated = append(updated, root.Content[:insertAt]...)
	updated = append(updated, keyNode, valueNode)
	updated = append(updated, root.Content[insertAt:]...)
	root.Content = updated
	return nil
}

func mappingRoot(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	return root
}
