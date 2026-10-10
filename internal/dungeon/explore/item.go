// Package explore reads every dungeon in a camp into a feed of finished work.
package explore

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

const (
	SchemaVersion = "camp-dungeon-explore/v1"

	KindFestival = "festival"
	KindWorkitem = "workitem"
	KindMarkdown = "markdown"
	KindOther    = "other"

	DateBucket   = "bucket"
	DateHistory  = "history"
	DateFileTime = "filetime"

	StatusHolding = "holding"
	LensAll       = "all"

	summaryLimit = 240
)

// Item is one row in the feed. Paths are camp-relative and use forward slashes.
type Item struct {
	DoneDate     string `json:"done_date"`
	DateSource   string `json:"date_source"`
	Status       string `json:"status"`
	DungeonLabel string `json:"dungeon_label"`
	DungeonPath  string `json:"dungeon_path"`
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	ID           string `json:"id,omitempty"`
	Summary      string `json:"summary,omitempty"`
	Path         string `json:"path"`
	Replay       string `json:"replay,omitempty"`
	Warning      string `json:"warning,omitempty"`
	IsDir        bool   `json:"is_dir,omitempty"`
}

// DungeonPrint is the freshness record for one dungeon directory.
type DungeonPrint struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Label       string `json:"label"`
	Fingerprint string `json:"fingerprint"`
}

// Index is every item Discover can see, plus the warnings from dungeons that
// could not be read completely.
type Index struct {
	Items    []Item         `json:"items"`
	Dungeons []DungeonPrint `json:"dungeons"`
	Warnings []string       `json:"warnings,omitempty"`
	BuiltAt  time.Time      `json:"built_at"`
}

// Query selects a slice of the index. Empty Status means the finished preset.
type Query struct {
	Status  string
	Dungeon string
	Since   string
	Until   string
	Text    string
}

// Result is the filtered feed and the names the header should show.
type Result struct {
	Items        []Item
	StatusLens   string
	DungeonLens  string
	DungeonLabel string
}

type statusPreset struct {
	name     string
	statuses []string
}

var builtinPresets = []statusPreset{
	{name: "finished", statuses: []string{"completed", "done"}},
	{name: "completed", statuses: []string{"completed"}},
	{name: "done", statuses: []string{"done"}},
	{name: "archived", statuses: []string{"archived"}},
	{name: "someday", statuses: []string{"someday"}},
	{name: "killed", statuses: []string{"killed"}},
	{name: "holding", statuses: []string{StatusHolding}},
	{name: LensAll, statuses: nil},
}

// Label is the human name of the directory that holds a dungeon.
func Label(parentRel string) string {
	switch parentRel {
	case ".", "":
		return "Camp"
	case "festivals":
		return "Festivals"
	case "workflow/design":
		return "Designs"
	case "workflow/explore":
		return "Explore"
	case "workflow/content":
		return "Content"
	case ".campaign/intents":
		return "Intents"
	case ".campaign/quests":
		return "Quests"
	default:
		return parentRel
	}
}

// Sort orders newest done date first, then dungeon label, then title.
func Sort(items []Item) {
	slices.SortStableFunc(items, func(a, b Item) int {
		if a.DoneDate != b.DoneDate {
			return cmp.Compare(b.DoneDate, a.DoneDate)
		}
		if c := cmp.Compare(a.DungeonLabel, b.DungeonLabel); c != 0 {
			return c
		}
		return cmp.Compare(a.Title, b.Title)
	})
}

// Presets lists the status lenses, including any status this index actually holds.
func Presets(items []Item) []string {
	names := make([]string, 0, len(builtinPresets))
	known := map[string]bool{}
	for _, p := range builtinPresets {
		names = append(names, p.name)
		known[p.name] = true
		for _, status := range p.statuses {
			known[status] = true
		}
	}
	extra := map[string]bool{}
	for _, item := range items {
		if item.Status != "" && !known[item.Status] && !extra[item.Status] {
			extra[item.Status] = true
		}
	}
	custom := make([]string, 0, len(extra))
	for name := range extra {
		custom = append(custom, name)
	}
	slices.Sort(custom)
	return append(names, custom...)
}

// NextPreset returns the status lens after name, wrapping through Presets.
func NextPreset(items []Item, name string) string {
	names := Presets(items)
	if len(names) == 0 {
		return "finished"
	}
	i := -1
	if resolved, ok := resolveStatus(name, items); ok {
		i = slices.Index(names, resolved)
	}
	if i < 0 {
		return names[0]
	}
	return names[(i+1)%len(names)]
}

// DungeonLenses returns "all" and then each dungeon path that has a row in
// the status lens, in path order.
func DungeonLenses(items []Item, status string) []string {
	want := statusesFor(status, items)
	seen := map[string]bool{}
	var paths []string
	for _, item := range items {
		if !statusMatch(item.Status, want) || seen[item.DungeonPath] {
			continue
		}
		seen[item.DungeonPath] = true
		paths = append(paths, item.DungeonPath)
	}
	slices.Sort(paths)
	return append([]string{LensAll}, paths...)
}

// StepDungeon moves the dungeon lens by delta through DungeonLenses.
func StepDungeon(items []Item, status, current string, delta int) string {
	lenses := DungeonLenses(items, status)
	if len(lenses) == 0 {
		return LensAll
	}
	i := slices.Index(lenses, current)
	if i < 0 {
		i = 0
	}
	n := len(lenses)
	next := (i + delta) % n
	if next < 0 {
		next += n
	}
	return lenses[next]
}

// Apply filters the index items. Status names and dungeon labels are matched
// case-insensitively. Dungeon paths match as stored. A dungeon resolves when
// the index discovered it, even if it holds no items.
func Apply(idx Index, q Query) (Result, error) {
	items := idx.Items
	status := q.Status
	if status == "" {
		status = "finished"
	}
	statusKey, ok := resolveStatus(status, items)
	if !ok {
		return Result{}, errUnknownStatus(status)
	}
	want := statusesFor(statusKey, items)

	dungeonKey, dungeonLabel, err := resolveDungeon(idx, q.Dungeon)
	if err != nil {
		return Result{}, err
	}
	since, until := q.Since, q.Until
	if since != "" && !validDay(since) {
		return Result{}, errBadDay("--since", since)
	}
	if until != "" && !validDay(until) {
		return Result{}, errBadDay("--until", until)
	}
	if since != "" && until != "" && since > until {
		return Result{}, errSinceAfterUntil()
	}
	text := strings.ToLower(strings.TrimSpace(q.Text))

	out := make([]Item, 0, len(items))
	for _, item := range items {
		if !statusMatch(item.Status, want) {
			continue
		}
		if dungeonKey != LensAll && item.DungeonPath != dungeonKey && !strings.EqualFold(item.DungeonLabel, dungeonKey) {
			continue
		}
		if since != "" && item.DoneDate < since {
			continue
		}
		if until != "" && item.DoneDate > until {
			continue
		}
		if text != "" && !matchesText(item, text) {
			continue
		}
		out = append(out, item)
	}
	return Result{
		Items:        out,
		StatusLens:   statusKey,
		DungeonLens:  dungeonKey,
		DungeonLabel: dungeonLabel,
	}, nil
}

// resolveStatus returns the preset or status directory name that raw names.
// An exact match wins, so differently cased custom statuses stay distinct.
func resolveStatus(raw string, items []Item) (string, bool) {
	names := Presets(items)
	if slices.Contains(names, raw) {
		return raw, true
	}
	for _, name := range names {
		if strings.EqualFold(name, raw) {
			return name, true
		}
	}
	return "", false
}

func statusesFor(name string, items []Item) []string {
	for _, p := range builtinPresets {
		if p.name == name {
			return p.statuses
		}
	}
	if slices.Contains(Presets(items), name) {
		return []string{name}
	}
	return nil
}

func statusMatch(status string, want []string) bool {
	if want == nil {
		return true
	}
	return slices.Contains(want, status)
}

func resolveDungeon(idx Index, raw string) (path, label string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, LensAll) {
		return LensAll, "All dungeons", nil
	}
	for _, d := range idx.Dungeons {
		if d.Path != raw && !strings.EqualFold(d.Label, raw) {
			continue
		}
		if path != "" && path != d.Path {
			return "", "", errAmbiguousDungeon(raw)
		}
		path, label = d.Path, d.Label
	}
	for _, item := range idx.Items {
		if item.DungeonPath != raw && !strings.EqualFold(item.DungeonLabel, raw) {
			continue
		}
		if path != "" && path != item.DungeonPath {
			return "", "", errAmbiguousDungeon(raw)
		}
		path = item.DungeonPath
		if label == "" {
			label = item.DungeonLabel
		}
	}
	if path == "" {
		return "", "", errUnknownDungeon(raw)
	}
	return path, label, nil
}

func matchesText(item Item, text string) bool {
	fields := []string{item.Title, item.ID, item.Summary, item.DungeonLabel, item.Path}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), text) {
			return true
		}
	}
	return false
}

func validDay(value string) bool {
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= summaryLimit {
		return s
	}
	return string(runes[:summaryLimit])
}
