package workitem

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

// gatherSource pairs a discovered workitem with its loaded .workitem
// metadata. Meta is nil when the directory has no marker file.
type gatherSource struct {
	Item wkitem.WorkItem
	Meta *wkitem.Metadata
}

// gatherPlan is the fully validated input to executeGather.
type gatherPlan struct {
	WorkflowType string
	Title        string
	Slug         string
	TargetAbs    string
	TargetRel    string
	Sources      []gatherSource
}

// gatherRootDir maps a gatherable workflow type to its campaign directory.
// Only directory-based builtin types can be gathered; intents have their own
// content-merge gather and festivals have lifecycle state that a directory
// move would corrupt.
func gatherRootDir(resolver *paths.Resolver, wfType string) (string, error) {
	switch wfType {
	case string(wkitem.WorkflowTypeDesign):
		return resolver.Design(), nil
	case string(wkitem.WorkflowTypeExplore):
		return resolver.Explore(), nil
	}
	return "", camperrors.NewValidation("type", "gather does not support workflow type "+wfType, nil)
}

// matchGatherSelector resolves one user-supplied selector against the typed
// candidate set, using the same precedence as the workitem selector package:
// exact stable id, exact key, exact relative path, exact directory slug.
func matchGatherSelector(items []wkitem.WorkItem, query string) (*wkitem.WorkItem, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, camperrors.NewValidation("selector", "selector must not be empty", nil)
	}

	type matcher struct {
		name string
		eq   func(wkitem.WorkItem) bool
	}
	matchers := []matcher{
		{"id", func(w wkitem.WorkItem) bool { return w.StableID != "" && w.StableID == q }},
		{"key", func(w wkitem.WorkItem) bool { return strings.EqualFold(w.Key, q) }},
		{"path", func(w wkitem.WorkItem) bool { return w.RelativePath == strings.TrimRight(q, "/") }},
		{"slug", func(w wkitem.WorkItem) bool { return strings.EqualFold(filepath.Base(w.RelativePath), q) }},
	}
	for _, m := range matchers {
		var matched []wkitem.WorkItem
		for _, item := range items {
			if m.eq(item) {
				matched = append(matched, item)
			}
		}
		if len(matched) == 1 {
			return &matched[0], nil
		}
		if len(matched) > 1 {
			keys := make([]string, 0, len(matched))
			for _, w := range matched {
				keys = append(keys, w.Key)
			}
			sort.Strings(keys)
			return nil, camperrors.NewValidation("selector",
				"selector "+q+" matched "+m.name+" against multiple workitems: "+strings.Join(keys, ", "), nil)
		}
	}
	return nil, camperrors.NewValidation("selector", "no workitem matched selector "+q, nil)
}

// dedupeGatherItems removes duplicate selections while preserving order.
func dedupeGatherItems(items []wkitem.WorkItem) []wkitem.WorkItem {
	seen := make(map[string]bool, len(items))
	out := make([]wkitem.WorkItem, 0, len(items))
	for _, item := range items {
		if seen[item.RelativePath] {
			continue
		}
		seen[item.RelativePath] = true
		out = append(out, item)
	}
	return out
}

// gatherBlockedByRun reports whether a source has a live fest workflow run
// that gathering would orphan mid-execution.
func gatherBlockedByRun(run *wkitem.LocalRunProgress) bool {
	if run == nil || run.ActiveRunID == "" {
		return false
	}
	switch run.RunStatus {
	case "completed", "abandoned":
		return false
	}
	return true
}

// gatherReadme renders the generated primary doc for the gathered package.
// It indexes each moved source so the new directory has a navigable entry
// point for both humans and workitem discovery.
func gatherReadme(title, wfType string, sources []wkitem.WorkItem) string {
	var b strings.Builder
	b.WriteString("# " + title + "\n\n")
	fmt.Fprintf(&b,
		"Gathered %s package combining %d workitems. Each source package was moved here unchanged and keeps its documents in the subdirectory listed below.\n\n",
		wfType, len(sources))
	b.WriteString("## Gathered packages\n\n")
	for _, item := range sources {
		b.WriteString(gatherReadmeEntry(item) + "\n")
	}
	return b.String()
}

func gatherReadmeEntry(item wkitem.WorkItem) string {
	base := filepath.Base(item.RelativePath)
	label := strings.TrimSpace(item.Title)
	if label == "" {
		label = base
	}
	entry := "- "
	if doc := gatherPrimaryDocRel(item); doc != "" {
		entry += "[" + label + "](" + doc + ")"
	} else {
		entry += label + " (" + base + "/)"
	}
	if summary := gatherSummaryLine(item.Summary); summary != "" {
		entry += ": " + summary
	}
	return entry
}

// gatherPrimaryDocRel returns the source's primary doc path relative to the
// gathered package root, or "" when the source has no primary doc inside its
// own directory.
func gatherPrimaryDocRel(item wkitem.WorkItem) string {
	if item.PrimaryDoc == "" {
		return ""
	}
	prefix := filepath.ToSlash(item.RelativePath) + "/"
	doc := filepath.ToSlash(item.PrimaryDoc)
	if !strings.HasPrefix(doc, prefix) {
		return ""
	}
	return filepath.Base(item.RelativePath) + "/" + strings.TrimPrefix(doc, prefix)
}

func gatherSummaryLine(summary string) string {
	return strings.Join(strings.Fields(summary), " ")
}

// migrateGatherPriorities moves manual priority state from the gathered
// sources onto the target key. The highest source priority wins; attention
// stages are dropped because a stage describes one item's position, which is
// not meaningful for the combined package. A group carries over only when
// every grouped source agrees on the same group. Returns true when the store
// changed.
func migrateGatherPriorities(store *priority.Store, sourceKeys []string, targetKey string) bool {
	changed := false
	best := priority.None
	groups := make(map[string]bool)
	for _, key := range sourceKeys {
		if entry, ok := store.ManualPriorities[key]; ok {
			if entry.Priority.Rank() < best.Rank() {
				best = entry.Priority
			}
			priority.Clear(store, key)
			changed = true
		}
		if entry, ok := store.Attention[key]; ok {
			if entry.Group != "" {
				groups[entry.Group] = true
			}
			priority.ClearAttentionStage(store, key)
			priority.ClearGroup(store, key)
			changed = true
		}
	}
	if best.Valid() {
		priority.Set(store, targetKey, best)
		changed = true
	}
	if len(groups) == 1 {
		for group := range groups {
			priority.SetGroup(store, targetKey, group)
		}
		changed = true
	}
	return changed
}

// rehomeGatherLinks re-points registry links from gathered source workitem
// ids to the gathered workitem id, then drops links that became exact
// duplicates (same workitem, scope, and role). Returns true when the
// registry changed.
func rehomeGatherLinks(reg *links.Links, idMap map[string]string) bool {
	changed := false
	for i := range reg.Links {
		if newID, ok := idMap[reg.Links[i].WorkitemID]; ok && newID != reg.Links[i].WorkitemID {
			reg.Links[i].WorkitemID = newID
			changed = true
		}
	}
	if !changed {
		return false
	}
	seen := make(map[string]bool, len(reg.Links))
	deduped := make([]links.Link, 0, len(reg.Links))
	for _, link := range reg.Links {
		key := link.WorkitemID + "|" + string(link.Scope.Kind) + "|" + link.Scope.Path + "|" + string(link.Role)
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, link)
	}
	reg.Links = deduped
	return true
}
