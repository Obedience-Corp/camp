package rename

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"gopkg.in/yaml.v3"
)

type metadataFile struct {
	store string
	abs   string
	rel   string
	kind  string
}

func planMetadata(root, oldName, newName, oldRel, newRel, remoteURL string, commitFiles []string) ([]MetadataChange, []string, error) {
	files, err := metadataFiles(root, oldName)
	if err != nil {
		return nil, nil, err
	}
	var changes []MetadataChange
	for _, file := range files {
		before, err := os.ReadFile(file.abs)
		if err != nil {
			return nil, nil, err
		}
		after, records, err := rewriteMetadata(file.kind, before, oldName, newName, oldRel, newRel, remoteURL)
		if err != nil {
			return nil, nil, camperrors.Wrapf(err, "rewrite %s", file.rel)
		}
		if records == 0 || bytes.Equal(before, after) {
			continue
		}
		changes = append(changes, MetadataChange{Store: file.store, Path: file.rel, Records: records})
		commitFiles = append(commitFiles, file.rel)
	}

	oldSnapshots := filepath.Join(root, ".campaign", "leverage", "snapshots", oldName)
	if info, err := os.Stat(oldSnapshots); err == nil && info.IsDir() {
		newSnapshotRel := filepath.ToSlash(filepath.Join(".campaign", "leverage", "snapshots", newName))
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(newSnapshotRel))); err == nil {
			return nil, nil, camperrors.Newf("leverage snapshot destination already exists: %s", newSnapshotRel)
		}
		count := 0
		_ = filepath.WalkDir(oldSnapshots, func(_ string, d fs.DirEntry, walkErr error) error {
			if walkErr == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".json") {
				count++
			}
			return nil
		})
		changes = append(changes, MetadataChange{
			Store:   "leverage-snapshots",
			Path:    filepath.ToSlash(filepath.Join(".campaign", "leverage", "snapshots", oldName)),
			Records: count,
		})
		commitFiles = append(commitFiles,
			filepath.ToSlash(filepath.Join(".campaign", "leverage", "snapshots", oldName)),
			newSnapshotRel,
		)
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, uniqueStrings(commitFiles), nil
}

func metadataFiles(root, oldName string) ([]metadataFile, error) {
	candidates := []metadataFile{
		{store: "campaign", rel: ".campaign/campaign.yaml", kind: "campaign-yaml"},
		{store: "fresh", rel: ".campaign/settings/fresh.yaml", kind: "fresh-yaml"},
		{store: "jumps", rel: ".campaign/settings/jumps.yaml", kind: "jumps-yaml"},
		{store: "pins", rel: ".campaign/settings/pins.json", kind: "pins-json"},
		{store: "workitem-links", rel: ".campaign/workitems/links.yaml", kind: "links-yaml"},
		{store: "leverage", rel: ".campaign/leverage/config.json", kind: "leverage-json"},
	}
	var out []metadataFile
	for _, candidate := range candidates {
		candidate.abs = filepath.Join(root, filepath.FromSlash(candidate.rel))
		if info, err := os.Stat(candidate.abs); err == nil && !info.IsDir() {
			out = append(out, candidate)
		}
	}

	walkRoots := []string{"workflow", "festivals", filepath.Join(".campaign", "intents")}
	for _, relRoot := range walkRoots {
		absRoot := filepath.Join(root, relRoot)
		if _, err := os.Stat(absRoot); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if d.Name() == ".git" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() != ".workitem" {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out = append(out, metadataFile{store: "workitem", abs: path, rel: filepath.ToSlash(rel), kind: "workitem-yaml"})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	oldSnapshotDir := filepath.Join(root, ".campaign", "leverage", "snapshots", oldName)
	if _, err := os.Stat(oldSnapshotDir); err == nil {
		err := filepath.WalkDir(oldSnapshotDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			out = append(out, metadataFile{store: "leverage-snapshot", abs: path, rel: filepath.ToSlash(rel), kind: "snapshot-json"})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func rewriteMetadata(kind string, raw []byte, oldName, newName, oldRel, newRel, remoteURL string) ([]byte, int, error) {
	switch kind {
	case "campaign-yaml", "fresh-yaml", "jumps-yaml", "links-yaml", "workitem-yaml":
		return rewriteYAML(kind, raw, oldName, newName, oldRel, newRel, remoteURL)
	case "pins-json", "leverage-json", "snapshot-json":
		return rewriteJSON(kind, raw, oldName, newName, oldRel, newRel)
	default:
		return raw, 0, nil
	}
}

func rewriteYAML(kind string, raw []byte, oldName, newName, oldRel, newRel, remoteURL string) ([]byte, int, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, 0, err
	}
	// A scaffolded settings file may contain documentation comments only.
	// With no typed values to migrate, preserve those bytes exactly.
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return raw, 0, nil
	}
	root := yamlDocumentMapping(&doc)
	if root == nil {
		return nil, 0, camperrors.New("top-level value is not a mapping")
	}
	count := 0
	switch kind {
	case "campaign-yaml":
		count += rewriteCampaignProjects(root, oldName, newName, oldRel, newRel, remoteURL)
		if projects := mappingPath(root, "commit", "guards", "projects"); projects != nil {
			if mappingContains(projects, oldName) && mappingContains(projects, newName) {
				return nil, 0, camperrors.Newf("commit guard project %q already exists", newName)
			}
			count += renameMappingKey(projects, oldName, newName)
		}
	case "fresh-yaml":
		if projects := mappingValue(root, "projects"); projects != nil {
			if mappingContains(projects, oldName) && mappingContains(projects, newName) {
				return nil, 0, camperrors.Newf("fresh project %q already exists", newName)
			}
			count += renameMappingKey(projects, oldName, newName)
		}
	case "jumps-yaml":
		if shortcuts := mappingValue(root, "shortcuts"); shortcuts != nil {
			count += rewritePathScalars(shortcuts, oldName, newName, oldRel, newRel)
		}
	case "links-yaml":
		if links := mappingValue(root, "links"); links != nil && links.Kind == yaml.SequenceNode {
			for _, link := range links.Content {
				if scope := mappingValue(link, "scope"); scope != nil {
					count += rewriteNamedPath(scope, "path", oldName, newName, oldRel, newRel)
				}
			}
		}
	case "workitem-yaml":
		if projects := mappingValue(root, "projects"); projects != nil && projects.Kind == yaml.SequenceNode {
			for _, item := range projects.Content {
				if item.Kind == yaml.ScalarNode {
					if next, ok := replaceManagedPath(item.Value, oldName, newName, oldRel, newRel); ok {
						item.Value = next
						count++
					}
				}
			}
		}
	}
	if count == 0 {
		return raw, 0, nil
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(4)
	if err := enc.Encode(&doc); err != nil {
		return nil, 0, err
	}
	_ = enc.Close()
	return out.Bytes(), count, nil
}

func rewriteCampaignProjects(root *yaml.Node, oldName, newName, oldRel, newRel, remoteURL string) int {
	projects := mappingValue(root, "projects")
	if projects == nil || projects.Kind != yaml.SequenceNode {
		return 0
	}
	count := 0
	for _, item := range projects.Content {
		nameValue := mappingValue(item, "name")
		pathValue := mappingValue(item, "path")
		matched := (nameValue != nil && nameValue.Value == oldName) || (pathValue != nil && pathValue.Value == oldRel)
		if nameValue != nil && nameValue.Value == oldName {
			nameValue.Value = newName
			count++
		}
		count += rewriteNamedPath(item, "path", oldName, newName, oldRel, newRel)
		if matched && remoteURL != "" {
			if value := mappingValue(item, "url"); value != nil && value.Kind == yaml.ScalarNode && value.Value != remoteURL {
				value.Value = remoteURL
				count++
			}
		}
	}
	return count
}

func rewritePathScalars(node *yaml.Node, oldName, newName, oldRel, newRel string) int {
	count := 0
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if (key.Value == "path" || key.Value == "workdir") && value.Kind == yaml.ScalarNode {
				if next, ok := replaceManagedPath(value.Value, oldName, newName, oldRel, newRel); ok {
					value.Value = next
					count++
				}
			}
			count += rewritePathScalars(value, oldName, newName, oldRel, newRel)
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			count += rewritePathScalars(child, oldName, newName, oldRel, newRel)
		}
	}
	return count
}

func rewriteNamedPath(mapping *yaml.Node, key, oldName, newName, oldRel, newRel string) int {
	value := mappingValue(mapping, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return 0
	}
	if next, ok := replaceManagedPath(value.Value, oldName, newName, oldRel, newRel); ok {
		value.Value = next
		return 1
	}
	return 0
}

func rewriteJSON(kind string, raw []byte, oldName, newName, oldRel, newRel string) ([]byte, int, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, 0, err
	}
	count := 0
	switch kind {
	case "pins-json":
		if rows, ok := value.([]any); ok {
			for _, row := range rows {
				if m, ok := row.(map[string]any); ok {
					count += rewriteJSONPath(m, "path", oldName, newName, oldRel, newRel)
				}
			}
		}
	case "leverage-json":
		if root, ok := value.(map[string]any); ok {
			if projects, ok := root["projects"].(map[string]any); ok {
				if old, exists := projects[oldName]; exists {
					if _, conflict := projects[newName]; conflict {
						return nil, 0, camperrors.Newf("leverage project %q already exists", newName)
					}
					delete(projects, oldName)
					projects[newName] = old
					count++
				}
				for _, entry := range projects {
					if m, ok := entry.(map[string]any); ok {
						count += rewriteJSONPath(m, "path", oldName, newName, oldRel, newRel)
						count += rewriteJSONPath(m, "monorepo_path", oldName, newName, oldRel, newRel)
					}
				}
			}
		}
	case "snapshot-json":
		if root, ok := value.(map[string]any); ok {
			if project, ok := root["project"].(string); ok && project == oldName {
				root["project"] = newName
				count++
			}
		}
	}
	if count == 0 {
		return raw, 0, nil
	}
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, 0, err
	}
	return append(out, '\n'), count, nil
}

func rewriteJSONPath(m map[string]any, key, oldName, newName, oldRel, newRel string) int {
	raw, ok := m[key].(string)
	if !ok {
		return 0
	}
	if next, changed := replaceManagedPath(raw, oldName, newName, oldRel, newRel); changed {
		m[key] = next
		return 1
	}
	return 0
}

func replaceManagedPath(value, oldName, newName, oldRel, newRel string) (string, bool) {
	valueSlash := filepath.ToSlash(value)
	pairs := [][2]string{
		{oldRel, newRel},
		{filepath.ToSlash(filepath.Join(filepath.Dir(oldRel), "worktrees", oldName)), filepath.ToSlash(filepath.Join(filepath.Dir(newRel), "worktrees", newName))},
	}
	for _, pair := range pairs {
		if valueSlash == pair[0] {
			return pair[1], true
		}
		if strings.HasPrefix(valueSlash, pair[0]+"/") {
			return pair[1] + strings.TrimPrefix(valueSlash, pair[0]), true
		}
	}
	return value, false
}

func applyMetadata(plan *PlanResult) ([]string, error) {
	files, err := metadataFiles(plan.CampaignRoot, plan.OldName)
	if err != nil {
		return nil, err
	}
	var changed []string
	for _, file := range files {
		before, err := os.ReadFile(file.abs)
		if err != nil {
			return changed, err
		}
		after, records, err := rewriteMetadata(file.kind, before, plan.OldName, plan.NewName, plan.OldPath, plan.NewPath, remoteURLForMetadata(plan))
		if err != nil {
			return changed, camperrors.Wrapf(err, "rewrite %s", file.rel)
		}
		if records == 0 || bytes.Equal(before, after) {
			continue
		}
		if err := fsutil.WriteFileAtomically(file.abs, after, 0o644); err != nil {
			return changed, err
		}
		changed = append(changed, file.rel)
	}

	oldSnapshots := filepath.Join(plan.CampaignRoot, ".campaign", "leverage", "snapshots", plan.OldName)
	newSnapshots := filepath.Join(plan.CampaignRoot, ".campaign", "leverage", "snapshots", plan.NewName)
	if _, err := os.Stat(oldSnapshots); err == nil {
		if err := os.MkdirAll(filepath.Dir(newSnapshots), 0o755); err != nil {
			return changed, err
		}
		if err := os.Rename(oldSnapshots, newSnapshots); err != nil {
			return changed, err
		}
		changed = append(changed, filepath.ToSlash(filepath.Join(".campaign", "leverage", "snapshots", plan.OldName)))
	}
	return changed, nil
}

func remoteURLForMetadata(plan *PlanResult) string {
	if plan.NewURL != "" && plan.NewURL != plan.OldURL {
		return plan.NewURL
	}
	return ""
}

func yamlDocumentMapping(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 && doc.Content[0].Kind == yaml.MappingNode {
		return doc.Content[0]
	}
	return nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func mappingPath(mapping *yaml.Node, keys ...string) *yaml.Node {
	current := mapping
	for _, key := range keys {
		current = mappingValue(current, key)
		if current == nil {
			return nil
		}
	}
	return current
}

func renameMappingKey(mapping *yaml.Node, oldName, newName string) int {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return 0
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == newName {
			for j := 0; j+1 < len(mapping.Content); j += 2 {
				if mapping.Content[j].Value == oldName {
					return 0
				}
			}
		}
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == oldName {
			mapping.Content[i].Value = newName
			return 1
		}
	}
	return 0
}

func mappingContains(mapping *yaml.Node, key string) bool {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.ToSlash(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
