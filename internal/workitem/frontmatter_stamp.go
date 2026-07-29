package workitem

import (
	"bytes"
	"context"
	"os"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// frontmatterIndent is the block indentation used for all frontmatter we write.
// Two spaces is the near-universal markdown frontmatter convention (Hugo,
// Obsidian, Jekyll), so encoding at this width preserves existing 2-space
// foreign keys byte-for-byte through a yaml.Node round-trip; yaml's default of 4
// would reflow them.
const frontmatterIndent = 2

// FrontmatterField is one camp-owned key to stamp into a document's frontmatter.
// Exactly one of Value (scalar keys like id/type/ref) or Values (list keys like
// tags/projects) is used; Values takes precedence when non-nil.
type FrontmatterField struct {
	After  string
	Key    string
	Value  string
	Values []string
}

func (f FrontmatterField) node() *yaml.Node {
	if f.Values != nil {
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, v := range f.Values {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
		}
		return seq
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: f.Value}
}

// StampFrontmatterFields updates the kind:workitem-style frontmatter block of the
// markdown file at path, inserting or replacing only the given camp-owned keys
// and leaving the body and all foreign keys untouched. It holds a per-file lock
// for the duration and writes atomically. The file must already have a
// frontmatter block (--- at byte zero); callers prepend a block first for
// no-frontmatter files.
func StampFrontmatterFields(ctx context.Context, path string, fields []FrontmatterField) error {
	release, err := fsutil.AcquireFileLock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()

	content, err := os.ReadFile(path)
	if err != nil {
		return camperrors.Wrapf(err, "read %s", path)
	}
	block, body, ok := SplitFrontmatter(content)
	if !ok {
		return camperrors.NewValidation("frontmatter",
			"no frontmatter block at the head of "+path, nil)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(block, &doc); err != nil {
		return camperrors.Wrapf(err, "parse frontmatter %s", path)
	}
	for _, f := range fields {
		if err := insertNodeAfter(&doc, f.After, f.Key, f.node()); err != nil {
			return err
		}
	}
	encoded, err := encodeFrontmatterNode(&doc)
	if err != nil {
		return camperrors.Wrapf(err, "encode frontmatter %s", path)
	}
	return fsutil.WriteFileAtomically(path, assembleFrontmatter(encoded, body), 0o644)
}

// MarshalMetadataFrontmatter renders meta as a frontmatter YAML block (no fences)
// at the frontmatter indent width, for the no-existing-frontmatter and
// create --file paths that prepend a full block.
func MarshalMetadataFrontmatter(meta *Metadata) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(frontmatterIndent)
	if err := enc.Encode(meta); err != nil {
		return nil, camperrors.Wrap(err, "encode metadata frontmatter")
	}
	if err := enc.Close(); err != nil {
		return nil, camperrors.Wrap(err, "close metadata frontmatter encoder")
	}
	return buf.Bytes(), nil
}

// FenceFrontmatter wraps an encoded frontmatter block in --- fences with a
// trailing blank line, ready to prepend to a body.
func FenceFrontmatter(block []byte) []byte {
	out := make([]byte, 0, len(block)+len("---\n---\n\n"))
	out = append(out, "---\n"...)
	out = append(out, block...)
	out = append(out, "---\n\n"...)
	return out
}

// FrontmatterHasKey reports whether the frontmatter block of content declares a
// top-level key. Used to decide the ambiguous tags-collision refusal and to
// avoid clobbering a foreign title.
func FrontmatterHasKey(content []byte, key string) bool {
	block, _, ok := SplitFrontmatter(content)
	if !ok {
		return false
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(block, &doc); err != nil {
		return false
	}
	root := mappingRoot(&doc)
	if root == nil {
		return false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Kind == yaml.ScalarNode && root.Content[i].Value == key {
			return true
		}
	}
	return false
}

// FrontmatterSequenceStrings returns the string scalar values of a top-level
// sequence key in content's frontmatter, plus the raw text of any entries that
// are not plain string scalars (numbers, bools, nested nodes) and so cannot be
// tags. A non-sequence value for the key is reported entirely as non-conforming.
func FrontmatterSequenceStrings(content []byte, key string) (values, nonConforming []string) {
	block, _, ok := SplitFrontmatter(content)
	if !ok {
		return nil, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(block, &doc); err != nil {
		return nil, nil
	}
	root := mappingRoot(&doc)
	if root == nil {
		return nil, nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Kind != yaml.ScalarNode || root.Content[i].Value != key {
			continue
		}
		valNode := root.Content[i+1]
		if valNode.Kind != yaml.SequenceNode {
			if valNode.Value != "" {
				nonConforming = append(nonConforming, valNode.Value)
			}
			return values, nonConforming
		}
		for _, e := range valNode.Content {
			if e.Kind == yaml.ScalarNode && e.Tag == "!!str" {
				values = append(values, e.Value)
			} else if e.Value != "" {
				nonConforming = append(nonConforming, e.Value)
			}
		}
		return values, nonConforming
	}
	return nil, nil
}

// FrontmatterMinIndent returns the smallest positive leading-space indent among
// the lines of content's frontmatter block, and whether any indented line
// exists. A block whose nested content is not 2-space indented will be reflowed
// to 2 spaces on re-encode, so callers warn when this is not 2.
func FrontmatterMinIndent(content []byte) (width int, hasIndent bool) {
	block, _, ok := SplitFrontmatter(content)
	if !ok {
		return 0, false
	}
	minIndent := -1
	for _, line := range bytes.Split(block, []byte("\n")) {
		trimmed := bytes.TrimLeft(line, " ")
		if len(trimmed) == 0 {
			continue
		}
		lead := len(line) - len(trimmed)
		if lead == 0 {
			continue
		}
		if minIndent < 0 || lead < minIndent {
			minIndent = lead
		}
	}
	if minIndent < 0 {
		return 0, false
	}
	return minIndent, true
}

func encodeFrontmatterNode(node *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(frontmatterIndent)
	if err := enc.Encode(node); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func assembleFrontmatter(encodedBlock, body []byte) []byte {
	out := make([]byte, 0, len(encodedBlock)+len(body)+len("---\n---\n"))
	out = append(out, "---\n"...)
	out = append(out, encodedBlock...)
	out = append(out, "---\n"...)
	out = append(out, body...)
	return out
}

// SplitFrontmatter divides full file content into the raw YAML block and the
// body that follows the closing delimiter line. ok is false when content does
// not begin with --- at byte zero or has no closing delimiter.
func SplitFrontmatter(content []byte) (block, body []byte, ok bool) {
	var openLen int
	switch {
	case bytes.HasPrefix(content, []byte("---\n")):
		openLen = len("---\n")
	case bytes.HasPrefix(content, []byte("---\r\n")):
		openLen = len("---\r\n")
	default:
		return nil, nil, false
	}
	rest := content[openLen:]
	idx := bytes.Index(rest, []byte("\n---"))
	if idx < 0 {
		return nil, nil, false
	}
	block = rest[:idx]
	closing := rest[idx+1:] // starts at the closing "---" line
	if nl := bytes.IndexByte(closing, '\n'); nl >= 0 {
		body = closing[nl+1:]
	}
	return block, body, true
}

// insertNodeAfter inserts {key: value} into the top-level mapping immediately
// after the pair whose key equals `after`, or replaces the value in place when
// key already exists, or appends when `after` is absent. Generalizes
// insertScalarAfter (ref_backfill.go) to an arbitrary value node so list keys
// (tags/projects) stamp the same way as scalars.
func insertNodeAfter(doc *yaml.Node, after, key string, value *yaml.Node) error {
	root := mappingRoot(doc)
	if root == nil {
		return camperrors.NewValidation("doc", "top-level YAML is not a mapping", nil)
	}
	insertAt := len(root.Content)
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			root.Content[i+1] = value
			return nil
		}
		if k.Kind == yaml.ScalarNode && k.Value == after {
			insertAt = i + 2
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	updated := make([]*yaml.Node, 0, len(root.Content)+2)
	updated = append(updated, root.Content[:insertAt]...)
	updated = append(updated, keyNode, value)
	updated = append(updated, root.Content[insertAt:]...)
	root.Content = updated
	return nil
}
