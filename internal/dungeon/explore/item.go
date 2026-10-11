// Package explore reads every dungeon in a camp into a feed of finished work.
package explore

import (
	"cmp"
	"path"
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
	// DateUnknown marks a row with no done date: the entry could not be
	// examined, so not even its file time is known.
	DateUnknown = "unknown"

	StatusHolding = "holding"
	LensAll       = "all"

	// A metadata file is read up to 256 KB, and any scalar in it can be that
	// long. These bound what one row puts in the index, the JSON, and the
	// cache. Real titles in a camp run past a thousand characters.
	titleLimit   = 2000
	idLimit      = 200
	summaryLimit = 240
	warningLimit = 400

	// maxIndexWarnings bounds the index-level list in a camp where every
	// directory fails to read.
	maxIndexWarnings = 100

	// directoryLens marks a status lens that selects one status directory by
	// name. No directory name contains it, so it cannot collide with one.
	directoryLens = "/"
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

// Index is every item the sweep can see, plus warnings for directories that
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

// Sort orders newest done date first, then dungeon label, then title. A row
// with no done date sorts after every dated one.
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

// Presets lists the status lenses, including any status this index actually
// holds. A status directory whose name is already a lens with another meaning
// (finished, all) is listed with a trailing slash.
func Presets(items []Item) []string {
	names := make([]string, 0, len(builtinPresets))
	for _, p := range builtinPresets {
		names = append(names, p.name)
	}
	extra := map[string]bool{}
	for _, item := range items {
		if item.Status != "" && !slices.Contains(names, lensFor(item.Status)) {
			extra[lensFor(item.Status)] = true
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

// Validate reports a date bound that is not YYYY-MM-DD, or a range that ends
// before it starts. Unlike the lenses, the dates need no index to check.
func (q Query) Validate() error {
	if q.Since != "" && !validDay(q.Since) {
		return errBadDay("since", q.Since)
	}
	if q.Until != "" && !validDay(q.Until) {
		return errBadDay("until", q.Until)
	}
	if q.Since != "" && q.Until != "" && q.Since > q.Until {
		return errSinceAfterUntil()
	}
	return nil
}

// Apply filters the index items. Status names and dungeon labels are matched
// case-insensitively. Dungeon paths match as stored. A dungeon resolves when
// the index discovered it, even if it holds no items. A row with no done
// date is outside every --since and --until range.
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
	if err := q.Validate(); err != nil {
		return Result{}, err
	}
	since, until := q.Since, q.Until
	text := strings.ToLower(strings.TrimSpace(q.Text))

	out := make([]Item, 0, len(items))
	for _, item := range items {
		if !statusMatch(item.Status, want) {
			continue
		}
		if dungeonKey != LensAll && item.DungeonPath != dungeonKey {
			continue
		}
		if item.DoneDate == "" && (since != "" || until != "") {
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

// lensFor is the lens that selects exactly one status directory. It is the
// directory name, unless a built-in lens of that name means something else.
func lensFor(status string) string {
	for _, p := range builtinPresets {
		if p.name == status && !slices.Equal(p.statuses, []string{status}) {
			return status + directoryLens
		}
	}
	return status
}

func resolveStatus(raw string, items []Item) (string, bool) {
	// A trailing slash always names a status directory, never a built-in lens.
	if dir, ok := strings.CutSuffix(raw, directoryLens); ok {
		return resolveDirectory(dir, items)
	}
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

func resolveDirectory(dir string, items []Item) (string, bool) {
	folded := ""
	for _, item := range items {
		if item.Status == dir {
			return lensFor(dir), true
		}
		if folded == "" && strings.EqualFold(item.Status, dir) {
			folded = item.Status
		}
	}
	if folded == "" {
		return "", false
	}
	return lensFor(folded), true
}

func statusesFor(name string, items []Item) []string {
	if dir, ok := strings.CutSuffix(name, directoryLens); ok {
		return []string{dir}
	}
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

// resolveDungeon turns a --dungeon value, or a lens the feed stepped to, into
// one dungeon path. A path the index holds is that dungeon exactly as
// written: nothing is trimmed from it and no label is weighed against it, so
// every lens DungeonLenses offers resolves to itself. Only then is the value
// read as typed text: trimmed, cleaned as a path, and last tried as a label,
// which unlike a path can name more than one dungeon.
func resolveDungeon(idx Index, raw string) (path, label string, err error) {
	if raw == "" {
		return LensAll, allDungeons, nil
	}
	if label, ok := labelAt(idx, raw); ok {
		return raw, label, nil
	}
	typed := strings.TrimSpace(raw)
	if typed == "" || strings.EqualFold(typed, LensAll) {
		return LensAll, allDungeons, nil
	}
	name := storedPath(typed)
	if label, ok := labelAt(idx, name); ok {
		return name, label, nil
	}
	for _, d := range idx.Dungeons {
		if !strings.EqualFold(d.Label, typed) && !strings.EqualFold(d.Label, name) {
			continue
		}
		if path != "" && path != d.Path {
			return "", "", errAmbiguousDungeon(typed)
		}
		path, label = d.Path, d.Label
	}
	for _, item := range idx.Items {
		if !strings.EqualFold(item.DungeonLabel, typed) && !strings.EqualFold(item.DungeonLabel, name) {
			continue
		}
		if path != "" && path != item.DungeonPath {
			return "", "", errAmbiguousDungeon(typed)
		}
		path, label = item.DungeonPath, item.DungeonLabel
	}
	if path == "" {
		return "", "", errUnknownDungeon(typed)
	}
	return path, label, nil
}

const allDungeons = "All dungeons"

// labelAt is the label of the dungeon stored at path. A dungeon the sweep
// found is known even when it holds no rows.
func labelAt(idx Index, path string) (string, bool) {
	for _, d := range idx.Dungeons {
		if d.Path == path {
			return d.Label, true
		}
	}
	for _, item := range idx.Items {
		if item.DungeonPath == path {
			return item.DungeonLabel, true
		}
	}
	return "", false
}

// storedPath is a path as the index spells it. One typed or completed by a
// shell may open with ./ or end in a slash. A label that is itself a path,
// such as workflow/pitch, is given the same room.
func storedPath(raw string) string {
	return path.Clean(raw)
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
	return capRunes(strings.Join(strings.Fields(s), " "), summaryLimit)
}

// capRunes cuts s to at most limit runes.
func capRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
