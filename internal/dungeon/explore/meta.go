package explore

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const metaReadLimit = 256 << 10

// lstat is how a metadata file is looked for. Tests replace it to report a
// directory the process cannot search.
var lstat = os.Lstat

func fillDirectory(ctx context.Context, item *Item, dir string) {
	base := filepath.Base(dir)
	workitem := filepath.Join(dir, ".workitem")
	// A directory that cannot be searched looks empty: every file below would
	// read as missing. The row says why it has no metadata.
	if _, err := lstat(workitem); err != nil && !camperrors.Is(err, fs.ErrNotExist) {
		item.Warning = readWarning(dir, err)
		return
	}
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

func fillFestival(ctx context.Context, item *Item, festYAML, goalFile string) string {
	named := false
	warning := ""
	if fileExists(festYAML) {
		// A file that reads in part keeps what it gave and its warning.
		meta, err := readFestMeta(ctx, festYAML)
		if err != nil {
			warning = readWarning(festYAML, err)
		}
		if meta.name != "" {
			item.Title = meta.name
			named = true
		}
		if meta.id != "" {
			item.ID = meta.id
		}
		if meta.goal != "" {
			item.Summary = meta.goal
		}
		if item.DateSource != DateBucket {
			if day, ok := newestDungeonDay(meta.history); ok {
				item.DoneDate = day
				item.DateSource = DateHistory
			}
		}
	}
	// The goal file supplies whatever fest.yaml left out, including all of it
	// when fest.yaml could not be read.
	if (item.Summary == "" || item.ID == "" || !named || item.DateSource == DateFileTime) && fileExists(goalFile) {
		fm, body, err := readFrontmatter(ctx, goalFile)
		if err != nil && warning == "" {
			warning = readWarning(goalFile, err)
		}
		// fest stamps the goal file with the move it records in fest.yaml.
		if item.DateSource == DateFileTime && strings.HasPrefix(fm["fest_status"], "dungeon/") {
			if day, ok := parseStamp(fm["fest_updated"]); ok {
				item.DoneDate = day
				item.DateSource = DateHistory
			}
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
	Status    string
	Timestamp string
}

// newestDungeonDay reads the day in the stamp's own offset. That is the
// mover's calendar day, the day a date bucket is named for, in whatever zone
// the feed is opened.
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

func fillWorkitem(ctx context.Context, item *Item, marker, readme string) string {
	warning := ""
	meta, err := readWorkitemMeta(ctx, marker)
	if err != nil {
		warning = readWarning(marker, err)
	}
	if meta.title != "" {
		item.Title = meta.title
	}
	if meta.ref != "" {
		item.ID = meta.ref
	} else if meta.id != "" {
		item.ID = meta.id
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

// primaryGoal is the sentence a goal file gives after its Primary Goal label,
// on that line or, when the label ends the line, in the text right under it.
func primaryGoal(body string) string {
	const marker = "**Primary Goal:**"
	_, after, found := strings.Cut(body, marker)
	if !found {
		return firstParagraph(body)
	}
	line, below, _ := strings.Cut(after, "\n")
	if goal := strings.TrimSpace(line); goal != "" {
		return goal
	}
	return textBelow(below)
}

// textBelow is the paragraph that opens body, after blank lines and HTML
// comments. A heading or another bold label there starts something else.
func textBelow(body string) string {
	var lines []string
	inComment := false
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if len(lines) == 0 {
			trim, inComment = skipComments(trim, inComment)
			if trim == "" {
				continue
			}
			if strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, "**") {
				return ""
			}
		}
		if trim == "" {
			break
		}
		lines = append(lines, trim)
	}
	return strings.Join(lines, " ")
}

// templateMarkers open the text fest leaves in a template for an author to
// replace. A file that still holds one there has no summary yet.
var templateMarkers = []string{"[REPLACE:", "[FILL:", "[GUIDANCE:", "{{ "}

func unfilled(summary string) bool {
	for _, marker := range templateMarkers {
		if strings.HasPrefix(summary, marker) {
			return true
		}
	}
	return false
}

// firstParagraph is the first run of text lines after any headings and HTML
// comments. Templates open a section with a comment that prompts the author.
func firstParagraph(body string) string {
	var lines []string
	started, inComment := false, false
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if !started {
			trim, inComment = skipComments(trim, inComment)
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

const (
	commentOpen  = "<!--"
	commentClose = "-->"
)

// skipComments drops the HTML comments a line opens with and returns what
// follows them. inComment carries a comment across lines.
func skipComments(line string, inComment bool) (string, bool) {
	for {
		if !inComment {
			rest, opens := strings.CutPrefix(line, commentOpen)
			if !opens {
				return line, false
			}
			line = rest
		}
		_, after, closed := strings.Cut(line, commentClose)
		if !closed {
			return "", true
		}
		line, inComment = strings.TrimSpace(after), false
	}
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
// without the raw block reaching a summary. A block that is never closed
// returns an error and no body: nothing says where it ends.
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
			return nil, "", errFrontmatterOpen
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
	root, err := parseYAML(raw)
	if err != nil {
		return nil, camperrors.Wrap(err, "frontmatter")
	}
	fm := map[string]string{}
	if root == nil {
		return fm, nil
	}
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

func readLimited(ctx context.Context, path string) (string, error) {
	data, err := ReadRegular(ctx, path, metaReadLimit)
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(data)), nil
}

func fileExists(path string) bool {
	info, err := lstat(path)
	return err == nil && info.Mode().IsRegular()
}
