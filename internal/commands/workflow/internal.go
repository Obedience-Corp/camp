package workflow

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/nav"
)

// builtinWorkflowTypes are workflow types reserved by the campaign system.
// They are excluded from user-workflow enumeration even if a concept or
// directory happens to exist for them.
//
// `intent`, `design`, `explore`, `festival` are the workitem builtins;
// `reviews` is scaffolded as an auto-source shortcut in camp's default config
// (see internal/config/defaults.go) and is not a user-created workflow
// collection. Legacy scaffold types remain reserved for compatibility.
var builtinWorkflowTypes = map[string]bool{
	"intent":       true,
	"design":       true,
	"explore":      true,
	"festival":     true,
	"reviews":      true,
	"code_reviews": true,
	"pipelines":    true,
}

// workflowEntry is a unified view of one user-created workflow collection.
type workflowEntry struct {
	Type          string
	Path          string // relative, with trailing slash, e.g. "workflow/research/"
	Title         string
	Category      string
	HasConcept    bool
	HasDir        bool
	ShortcutKey   string
	ShortcutPath  string
	HasShortcut   bool
	WorkitemCount int
	LastModified  time.Time
}

// enumerateWorkflowEntries returns the union of concept-listed workflows and
// on-disk workflow/<type>/ directories. Builtin types are filtered out.
// Entries are sorted by type name.
// flattenConcepts returns every concept including nested children, depth-first.
func flattenConcepts(concepts []config.ConceptEntry) []config.ConceptEntry {
	var out []config.ConceptEntry
	for _, c := range concepts {
		out = append(out, c)
		if len(c.Children) > 0 {
			out = append(out, flattenConcepts(c.Children)...)
		}
	}
	return out
}

// collectWorkflowConcepts records non-builtin workflow/ concepts, descending
// into nested children so collections under the workflow parent are found.
func collectWorkflowConcepts(concepts []config.ConceptEntry, entries map[string]*workflowEntry) {
	for _, concept := range concepts {
		if strings.HasPrefix(concept.Path, "workflow/") {
			if typeName := workflowTypeFromPath(concept.Path); typeName != "" && !builtinWorkflowTypes[typeName] {
				entry := entries[typeName]
				if entry == nil {
					entry = &workflowEntry{Type: typeName, Path: concept.Path}
					entries[typeName] = entry
				}
				entry.HasConcept = true
				if entry.Title == "" {
					entry.Title = stripWorkflowSuffix(concept.Description)
				}
			}
		}
		if len(concept.Children) > 0 {
			collectWorkflowConcepts(concept.Children, entries)
		}
	}
}

func enumerateWorkflowEntries(campaignRoot string, cfg *config.CampaignConfig) ([]workflowEntry, error) {
	entries := make(map[string]*workflowEntry)

	// Workflow concepts may be nested under the workflow parent, so descend.
	collectWorkflowConcepts(cfg.Concepts(), entries)

	workflowRoot := filepath.Join(campaignRoot, "workflow")
	dirEntries, err := os.ReadDir(workflowRoot)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, camperrors.Wrap(err, "read workflow root")
	}
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		typeName := de.Name()
		if strings.HasPrefix(typeName, ".") || builtinWorkflowTypes[typeName] {
			continue
		}
		entry := entries[typeName]
		if entry == nil {
			entry = &workflowEntry{
				Type: typeName,
				Path: path.Join("workflow", typeName) + "/",
			}
			entries[typeName] = entry
		}
		entry.HasDir = true
	}

	shortcuts := cfg.Shortcuts()
	for _, entry := range entries {
		entry.Category = cfg.WorkflowCategoryForType(entry.Type)
		if key, ok := findShortcutByPath(shortcuts, entry.Path); ok {
			entry.HasShortcut = true
			entry.ShortcutKey = key
			entry.ShortcutPath = shortcuts[key].Path
		}
	}

	result := make([]workflowEntry, 0, len(entries))
	for _, e := range entries {
		result = append(result, *e)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Type < result[j].Type })
	return result, nil
}

// populateWorkitemStats walks the workflow tree on disk and fills in
// WorkitemCount + LastModified for entries that have a directory.
func populateWorkitemStats(campaignRoot string, entries []workflowEntry) ([]workflowEntry, error) {
	for i := range entries {
		if !entries[i].HasDir {
			continue
		}
		abs := filepath.Join(campaignRoot, filepath.FromSlash(entries[i].Path))
		count, mtime, err := walkWorkflowDirCount(abs)
		if err != nil {
			return nil, err
		}
		entries[i].WorkitemCount = count
		entries[i].LastModified = mtime
	}
	return entries, nil
}

// walkWorkflowDirCount returns the number of .workitem markers under absPath
// and the most recent modification time of any marker.
func walkWorkflowDirCount(absPath string) (int, time.Time, error) {
	var (
		count int
		mtime time.Time
	)
	err := filepath.WalkDir(absPath, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if p != absPath && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != ".workitem" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		count++
		if info.ModTime().After(mtime) {
			mtime = info.ModTime()
		}
		return nil
	})
	if err != nil {
		return 0, time.Time{}, camperrors.Wrapf(err, "walk %s", absPath)
	}

	if mtime.IsZero() {
		if info, err := os.Stat(absPath); err == nil {
			mtime = info.ModTime()
		}
	}
	return count, mtime, nil
}

// findShortcutByPath returns the first shortcut key whose Path equals target,
// matched after normalizing trailing-slash.
func findShortcutByPath(shortcuts map[string]config.ShortcutConfig, target string) (string, bool) {
	normalize := func(s string) string { return strings.TrimRight(s, "/") }
	want := normalize(target)
	keys := make([]string, 0, len(shortcuts))
	for k := range shortcuts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if normalize(shortcuts[k].Path) == want {
			return k, true
		}
	}
	return "", false
}

// workflowTypeFromPath extracts the type name from a "workflow/<type>/" path.
// Returns "" if the path does not match.
func workflowTypeFromPath(relPath string) string {
	cleaned := strings.TrimRight(relPath, "/")
	parts := strings.Split(cleaned, "/")
	if len(parts) != 2 || parts[0] != "workflow" {
		return ""
	}
	return parts[1]
}

// stripWorkflowSuffix removes the " workflow" suffix that upsertShortcut and
// upsertConcept append to descriptions, recovering the user-supplied title.
func stripWorkflowSuffix(desc string) string {
	return strings.TrimSuffix(desc, " workflow")
}

// duplicateShortcutKeys returns groups of two-or-more shortcut keys that
// normalize to the same value.
func duplicateShortcutKeys(shortcuts map[string]config.ShortcutConfig) map[string][]string {
	grouped := make(map[string][]string)
	for key := range shortcuts {
		normalized := nav.NormalizeNavigationName(key)
		grouped[normalized] = append(grouped[normalized], key)
	}
	dupes := make(map[string][]string)
	for norm, keys := range grouped {
		if len(keys) >= 2 {
			sort.Strings(keys)
			dupes[norm] = keys
		}
	}
	return dupes
}
