package explore

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const metaReadLimit = 256 << 10

func fillDirectory(ctx context.Context, item *Item, dir string) {
	base := filepath.Base(dir)
	workitem := filepath.Join(dir, ".workitem")
	if fileExists(workitem) {
		item.Kind = KindWorkitem
		if warning := fillWorkitem(ctx, item, workitem, filepath.Join(dir, "README.md")); warning != "" {
			item.Warning = warning
		}
		return
	}
	replay := replayPath(dir, base)
	festYAML := filepath.Join(dir, "fest.yaml")
	goalFile := filepath.Join(dir, "FESTIVAL_GOAL.md")
	hasFest := fileExists(festYAML) || fileExists(goalFile) || replay != ""
	if hasFest {
		item.Kind = KindFestival
		if replay != "" {
			item.Replay = slashUnder(item.Path, filepath.Base(replay))
		}
		if warning := fillFestival(ctx, item, festYAML, goalFile); warning != "" {
			item.Warning = warning
		}
		if item.Title == "" || item.Title == base && item.ID == "" && item.Summary == "" {
			item.Title = base
		}
		return
	}
	readme := filepath.Join(dir, "README.md")
	if fileExists(readme) {
		fillMarkdown(ctx, item, readme)
		if item.Title == "" {
			item.Title = base
		}
		return
	}
	item.Title = base
}

func slashUnder(dirRel, name string) string {
	if dirRel == "" {
		return name
	}
	return dirRel + "/" + name
}

func replayPath(dir, base string) string {
	embedded := filepath.Join(dir, "festival-replay.gif")
	if fileExists(embedded) {
		return embedded
	}
	named := filepath.Join(dir, base+".gif")
	if fileExists(named) {
		return named
	}
	return ""
}

type festMeta struct {
	Metadata struct {
		ID            string         `yaml:"id"`
		Name          string         `yaml:"name"`
		Goal          string         `yaml:"goal"`
		StatusHistory []historyEntry `yaml:"status_history"`
	} `yaml:"metadata"`
}

func fillFestival(ctx context.Context, item *Item, festYAML, goalFile string) string {
	named := false
	warning := ""
	if fileExists(festYAML) {
		var meta festMeta
		if err := readYAML(ctx, festYAML, &meta); err != nil {
			warning = readWarning(festYAML, err)
			meta = festMeta{}
		}
		if meta.Metadata.Name != "" {
			item.Title = meta.Metadata.Name
			named = true
		}
		if meta.Metadata.ID != "" {
			item.ID = meta.Metadata.ID
		}
		if meta.Metadata.Goal != "" {
			item.Summary = meta.Metadata.Goal
		}
		if item.DateSource != DateBucket {
			if day, ok := newestDungeonDay(meta.Metadata.StatusHistory); ok {
				item.DoneDate = day
				item.DateSource = DateHistory
			}
		}
	}
	// The goal file supplies whatever fest.yaml left out, including all of it
	// when fest.yaml could not be read.
	if (item.Summary == "" || item.ID == "" || !named) && fileExists(goalFile) {
		fm, body, err := readFrontmatter(ctx, goalFile)
		if err != nil && warning == "" {
			warning = readWarning(goalFile, err)
		}
		if item.ID == "" {
			item.ID = fm["fest_id"]
		}
		if !named {
			if name := fm["fest_name"]; name != "" {
				item.Title = name
			}
		}
		if item.Summary == "" {
			item.Summary = primaryGoal(body)
		}
	}
	if item.Title == "" {
		item.Title = filepath.Base(filepath.Dir(festYAML))
	}
	return warning
}

type historyEntry struct {
	Status    string `yaml:"status"`
	Timestamp string `yaml:"timestamp"`
}

func newestDungeonDay(history []historyEntry) (string, bool) {
	var best time.Time
	found := false
	for _, entry := range history {
		if !strings.HasPrefix(entry.Status, "dungeon/") {
			continue
		}
		stamp, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err != nil {
			stamp, err = time.Parse(time.RFC3339Nano, entry.Timestamp)
		}
		if err != nil {
			continue
		}
		if !found || stamp.After(best) {
			best = stamp
			found = true
		}
	}
	if !found {
		return "", false
	}
	return best.Format("2006-01-02"), true
}

type workitemMeta struct {
	ID    string `yaml:"id"`
	Title string `yaml:"title"`
	Ref   string `yaml:"ref"`
}

func fillWorkitem(ctx context.Context, item *Item, marker, readme string) string {
	warning := ""
	var meta workitemMeta
	if err := readYAML(ctx, marker, &meta); err != nil {
		warning = readWarning(marker, err)
		meta = workitemMeta{}
	}
	if meta.Title != "" {
		item.Title = meta.Title
	}
	if meta.Ref != "" {
		item.ID = meta.Ref
	} else if meta.ID != "" {
		item.ID = meta.ID
	}
	// The README is read even when the marker could not be: it still holds
	// the summary.
	if fileExists(readme) {
		_, body, err := readFrontmatter(ctx, readme)
		if err != nil && warning == "" {
			warning = readWarning(readme, err)
		}
		item.Summary = firstParagraph(body)
	}
	return warning
}

func fillMarkdown(ctx context.Context, item *Item, path string) {
	fm, body, err := readFrontmatter(ctx, path)
	if err != nil {
		item.Warning = readWarning(path, err)
	}
	// A file that could not be read, or whose frontmatter would not parse,
	// stays a markdown row and carries the warning.
	if len(fm) == 0 && err == nil && item.Kind != KindFestival && item.Kind != KindWorkitem {
		item.Kind = KindOther
		item.Summary = firstParagraph(body)
		return
	}
	if item.Kind != KindFestival && item.Kind != KindWorkitem {
		item.Kind = KindMarkdown
	}
	if title := fm["title"]; title != "" {
		item.Title = title
	}
	if id := fm["id"]; id != "" && item.ID == "" {
		item.ID = id
	}
	if item.Summary == "" {
		item.Summary = firstParagraph(body)
	}
	if item.DateSource == DateBucket {
		return
	}
	status := fm["status"]
	if strings.HasPrefix(status, "dungeon/") {
		if stamp, ok := parseStamp(fm["updated_at"]); ok {
			item.DoneDate = stamp
			item.DateSource = DateHistory
		}
	}
}

// stampLayouts are the timestamp spellings YAML accepts for a plain scalar.
// Frontmatter is read as written, so updated_at is parsed here instead.
var stampLayouts = []string{
	time.RFC3339Nano,
	"2006-1-2T15:4:5.999999999Z07:00",
	"2006-1-2t15:4:5.999999999Z07:00",
	"2006-1-2 15:4:5.999999999",
	"2006-1-2",
}

func parseStamp(value string) (string, bool) {
	for _, layout := range stampLayouts {
		if stamp, err := time.Parse(layout, value); err == nil {
			return stamp.Format("2006-01-02"), true
		}
	}
	return "", false
}

func primaryGoal(body string) string {
	for _, line := range strings.Split(body, "\n") {
		const marker = "**Primary Goal:**"
		if idx := strings.Index(line, marker); idx >= 0 {
			return strings.TrimSpace(line[idx+len(marker):])
		}
	}
	return firstParagraph(body)
}

func firstParagraph(body string) string {
	var lines []string
	started := false
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if !started {
			if trim == "" || strings.HasPrefix(trim, "#") {
				continue
			}
			started = true
		}
		if trim == "" {
			break
		}
		lines = append(lines, trim)
	}
	return strings.Join(lines, " ")
}

// MarkdownBody returns text without a leading YAML frontmatter block. A block
// that does not parse is left in place for the reader to show.
func MarkdownBody(text string) string {
	_, body, err := splitFrontmatter(text)
	if err != nil {
		return text
	}
	return body
}

const (
	byteOrderMark    = "\U0000FEFF"
	frontmatterFence = "---"
)

// splitFrontmatter separates a leading YAML block from the body. The map is
// nil when text does not open with a fenced block. A fenced block that is not
// a YAML mapping returns an error and the body after it, so a caller can warn
// without the raw block reaching a summary.
func splitFrontmatter(text string) (map[string]string, string, error) {
	text = strings.TrimPrefix(text, byteOrderMark)
	first, rest, found := strings.Cut(text, "\n")
	if !found || !isFence(first) {
		return nil, text, nil
	}
	var raw strings.Builder
	for {
		line, after, more := strings.Cut(rest, "\n")
		if isFence(line) {
			fm, err := decodeFrontmatter(raw.String())
			return fm, after, err
		}
		if !more {
			return nil, text, nil
		}
		raw.WriteString(line)
		raw.WriteByte('\n')
		rest = after
	}
}

// isFence accepts trailing spaces, tabs, and a carriage return after the dashes.
func isFence(line string) bool {
	return strings.TrimRight(line, " \t\r") == frontmatterFence
}

// decodeFrontmatter reads each scalar value as the text the author wrote.
// Decoding into Go types would rewrite it: 0012 becomes 10, 1e3 becomes 1000,
// and a date becomes a timestamp. A key with no value, a null, or a list or
// mapping value is kept with an empty value.
func decodeFrontmatter(raw string) (map[string]string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, camperrors.Wrap(err, "frontmatter")
	}
	fm := map[string]string{}
	if len(doc.Content) == 0 {
		return fm, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, errFrontmatterShape
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			continue
		}
		if value.Kind == yaml.AliasNode && value.Alias != nil {
			value = value.Alias
		}
		fm[key.Value] = ""
		if value.Kind == yaml.ScalarNode && value.ShortTag() != "!!null" {
			fm[key.Value] = strings.TrimSpace(value.Value)
		}
	}
	return fm, nil
}

// readFrontmatter reads a markdown file and splits it. On a read error the
// map and body are empty.
func readFrontmatter(ctx context.Context, path string) (map[string]string, string, error) {
	text, err := readLimited(ctx, path)
	if err != nil {
		return nil, "", err
	}
	return splitFrontmatter(text)
}

// readWarning says which metadata file could not be used. It names the file
// alone: the row already carries the camp-relative path, and the error's
// absolute one must not reach the feed or its JSON.
func readWarning(path string, err error) string {
	return filepath.Base(path) + ": " + cause(err).Error()
}

func readYAML(ctx context.Context, path string, dest any) error {
	data, err := readLimited(ctx, path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal([]byte(data), dest)
}

func readLimited(ctx context.Context, path string) (string, error) {
	data, err := ReadRegular(ctx, path, metaReadLimit)
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(data)), nil
}

func fileExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}
