package workitem

import (
	"context"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// BackfillRef writes ref into a directory workitem marker if it is still empty.
func BackfillRef(ctx context.Context, root, relPath, ref string) error {
	abs := filepath.Join(root, filepath.FromSlash(relPath), MetadataFilename)

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
