package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// FollowUpNotFoundError is returned by RemoveFreshFollowUp when name does not
// match any configured follow-up step within the requested scope.
type FollowUpNotFoundError struct {
	Name       string
	Scope      string
	ValidNames []string
}

// Error implements the error interface, listing the valid names for the
// scope that was searched so the caller can correct a typo without a
// separate `configure show` round trip.
func (e *FollowUpNotFoundError) Error() string {
	if len(e.ValidNames) == 0 {
		return fmt.Sprintf("follow-up %q not found in %s scope (none configured there)", e.Name, e.Scope)
	}
	return fmt.Sprintf("follow-up %q not found in %s scope; valid names: %s",
		e.Name, e.Scope, strings.Join(e.ValidNames, ", "))
}

// AddFreshFollowUp appends a follow-up step to fresh.yaml. An empty
// projectName targets the global follow-up list; otherwise the step is added
// to the per-project override list for projectName, which replaces the
// global list entirely for that project.
//
// The write creates .campaign/settings/fresh.yaml (and its parent directory)
// if missing, and preserves every key it does not touch.
func AddFreshFollowUp(ctx context.Context, campaignRoot, projectName string, entry FollowUpConfig) error {
	if err := entry.Validate(); err != nil {
		return err
	}

	return withFreshConfigLock(ctx, campaignRoot, func(mapping *yaml.Node) error {
		seq, err := followUpSequence(mapping, projectName, true)
		if err != nil {
			return err
		}

		if findFollowUpIndex(seq, entry.Name) >= 0 {
			return camperrors.NewValidation("name", fmt.Sprintf(
				"follow-up %q already exists in %s scope; remove it first to redefine it",
				entry.Name, followUpScopeLabel(projectName)), nil)
		}

		var node yaml.Node
		if err := node.Encode(entry); err != nil {
			return camperrors.Wrap(err, "encode follow-up entry")
		}
		seq.Content = append(seq.Content, &node)
		return nil
	})
}

// RemoveFreshFollowUp removes the named follow-up step from fresh.yaml. An
// empty projectName targets the global follow-up list; otherwise the
// per-project override list for projectName. Returns a *FollowUpNotFoundError
// listing the valid names in that scope when name is not found.
//
// Once a scope's follow-up list becomes empty, the now-redundant follow_up
// key (and any now-empty project/projects wrapper) is dropped so the file
// stays clean.
func RemoveFreshFollowUp(ctx context.Context, campaignRoot, projectName, name string) error {
	return withFreshConfigLock(ctx, campaignRoot, func(mapping *yaml.Node) error {
		seq, err := followUpSequence(mapping, projectName, false)
		if err != nil {
			return err
		}

		idx := findFollowUpIndex(seq, name)
		if idx < 0 {
			return &FollowUpNotFoundError{
				Name:       name,
				Scope:      followUpScopeLabel(projectName),
				ValidNames: followUpNames(seq),
			}
		}

		seq.Content = append(seq.Content[:idx], seq.Content[idx+1:]...)
		pruneEmptyFollowUpScope(mapping, projectName, seq)
		return nil
	})
}

// withFreshConfigLock loads fresh.yaml as a raw YAML document (creating an
// empty mapping document if the file is missing or has no parseable
// mapping content), runs mutate against its top-level mapping, then writes
// the result back atomically under an exclusive file lock so concurrent
// `configure` invocations cannot interleave their read-modify-write cycles.
func withFreshConfigLock(ctx context.Context, campaignRoot string, mutate func(mapping *yaml.Node) error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	configPath := FreshConfigPath(campaignRoot)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return camperrors.Wrap(err, "create settings directory")
	}

	release, err := fsutil.AcquireFileLock(ctx, configPath+".lock")
	if err != nil {
		return err
	}
	defer release()

	return mutateFreshDoc(configPath, mutate)
}

func mutateFreshDoc(configPath string, mutate func(mapping *yaml.Node) error) error {
	raw, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return camperrors.Wrapf(err, "read fresh config %s", configPath)
	}

	var doc yaml.Node
	if err == nil {
		if uerr := yaml.Unmarshal(raw, &doc); uerr != nil {
			return camperrors.Wrapf(uerr, "parse fresh config %s", configPath)
		}
	}

	// A missing file, an empty file, and a file containing only comments
	// (the scaffolded fresh.yaml template ships fully commented out) all
	// decode to a zero-value Node: yaml.v3 has no mapping node to attach
	// top-level comments to, so it drops them rather than returning one.
	// Preserve the original bytes verbatim ahead of the mapping we build so
	// that documentation header is not silently discarded on first write.
	var headerPreserve []byte
	if doc.Kind == 0 {
		if len(bytes.TrimSpace(raw)) > 0 {
			headerPreserve = raw
		}
		doc = yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}},
		}
	}

	mapping := freshMappingRoot(&doc)
	if mapping == nil {
		return camperrors.NewValidation("fresh.yaml", "top-level content must be a mapping", nil)
	}

	if err := mutate(mapping); err != nil {
		return err
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return camperrors.Wrap(err, "marshal fresh config")
	}

	if len(headerPreserve) > 0 {
		combined := make([]byte, 0, len(headerPreserve)+len(out)+1)
		combined = append(combined, headerPreserve...)
		if !bytes.HasSuffix(headerPreserve, []byte("\n")) {
			combined = append(combined, '\n')
		}
		out = append(combined, out...)
	}

	return fsutil.WriteFileAtomically(configPath, out, 0o644)
}

// followUpSequence locates the follow_up sequence node for the given scope.
// An empty projectName targets the top-level follow_up key; otherwise the
// projects.<projectName>.follow_up key. When create is true, any missing
// mapping/sequence node along the path is created; when false, a missing
// node returns (nil, nil) rather than an error.
func followUpSequence(mapping *yaml.Node, projectName string, create bool) (*yaml.Node, error) {
	target := mapping
	if projectName != "" {
		projectsNode, err := mappingChild(target, "projects", yaml.MappingNode, "!!map", create)
		if err != nil || projectsNode == nil {
			return nil, err
		}
		projectNode, err := mappingChild(projectsNode, projectName, yaml.MappingNode, "!!map", create)
		if err != nil || projectNode == nil {
			return nil, err
		}
		target = projectNode
	}
	return mappingChild(target, "follow_up", yaml.SequenceNode, "!!seq", create)
}

// mappingChild returns the value node for key within mapping. If key is
// absent and create is false, it returns (nil, nil). If key is absent and
// create is true, a new node of the given kind/tag is appended and returned.
// An existing value whose Kind does not match is reported as an error rather
// than silently overwritten.
func mappingChild(mapping *yaml.Node, key string, kind yaml.Kind, tag string, create bool) (*yaml.Node, error) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k := mapping.Content[i]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			v := mapping.Content[i+1]
			if v.Kind != kind {
				return nil, camperrors.NewValidation(key, "has an unexpected shape in fresh.yaml", nil)
			}
			return v, nil
		}
	}
	if !create {
		return nil, nil
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{Kind: kind, Tag: tag}
	mapping.Content = append(mapping.Content, keyNode, valueNode)
	return valueNode, nil
}

// removeMappingKey removes key (and its value) from mapping, reporting
// whether it was present.
func removeMappingKey(mapping *yaml.Node, key string) bool {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Kind == yaml.ScalarNode && mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return true
		}
	}
	return false
}

// pruneEmptyFollowUpScope drops the now-redundant follow_up key once seq is
// empty, cascading up through an emptied project entry and, in turn, an
// emptied projects mapping, so removing the last follow-up leaves a clean
// fresh.yaml rather than dangling empty containers.
func pruneEmptyFollowUpScope(mapping *yaml.Node, projectName string, seq *yaml.Node) {
	if len(seq.Content) > 0 {
		return
	}
	if projectName == "" {
		removeMappingKey(mapping, "follow_up")
		return
	}

	projectsNode, _ := mappingChild(mapping, "projects", yaml.MappingNode, "!!map", false)
	if projectsNode == nil {
		return
	}
	projectNode, _ := mappingChild(projectsNode, projectName, yaml.MappingNode, "!!map", false)
	if projectNode == nil {
		return
	}

	removeMappingKey(projectNode, "follow_up")
	if len(projectNode.Content) == 0 {
		removeMappingKey(projectsNode, projectName)
	}
	if len(projectsNode.Content) == 0 {
		removeMappingKey(mapping, "projects")
	}
}

// findFollowUpIndex returns the index of the entry named name within seq, or
// -1 if seq is nil or has no such entry.
func findFollowUpIndex(seq *yaml.Node, name string) int {
	if seq == nil {
		return -1
	}
	for i, item := range seq.Content {
		if followUpEntryName(item) == name {
			return i
		}
	}
	return -1
}

// followUpNames collects the name field of every entry in seq, in order.
func followUpNames(seq *yaml.Node) []string {
	if seq == nil {
		return nil
	}
	names := make([]string, 0, len(seq.Content))
	for _, item := range seq.Content {
		if name := followUpEntryName(item); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func followUpEntryName(item *yaml.Node) string {
	if item == nil || item.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(item.Content); i += 2 {
		k := item.Content[i]
		v := item.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == "name" && v.Kind == yaml.ScalarNode {
			return v.Value
		}
	}
	return ""
}

func followUpScopeLabel(projectName string) string {
	if projectName == "" {
		return "global"
	}
	return "project " + projectName
}

func freshMappingRoot(doc *yaml.Node) *yaml.Node {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	return root
}
