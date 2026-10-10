package explore

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const metaReadLimit = 256 << 10

func fillDirectory(item *Item, dir string) {
	base := filepath.Base(dir)
	workitem := filepath.Join(dir, ".workitem")
	if fileExists(workitem) {
		item.Kind = KindWorkitem
		if warning := fillWorkitem(item, workitem, filepath.Join(dir, "README.md")); warning != "" {
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
		if warning := fillFestival(item, festYAML, goalFile); warning != "" {
			item.Warning = warning
		}
		if item.Title == "" || item.Title == base && item.ID == "" && item.Summary == "" {
			item.Title = base
		}
		return
	}
	readme := filepath.Join(dir, "README.md")
	if fileExists(readme) {
		fillMarkdown(item, readme)
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

func fillFestival(item *Item, festYAML, goalFile string) string {
	if fileExists(festYAML) {
		var meta festMeta
		if err := readYAML(festYAML, &meta); err != nil {
			item.Title = filepath.Base(filepath.Dir(festYAML))
			return err.Error()
		}
		if meta.Metadata.Name != "" {
			item.Title = meta.Metadata.Name
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
	if item.Summary == "" || item.ID == "" || item.Title == "" {
		if fileExists(goalFile) {
			text, err := readLimited(goalFile)
			if err != nil {
				return err.Error()
			}
			fm, body := splitFrontmatter(text)
			if item.ID == "" {
				item.ID = fm["fest_id"]
			}
			if item.Title == "" || item.Title == filepath.Base(filepath.Dir(goalFile)) {
				if name := fm["fest_name"]; name != "" {
					item.Title = name
				}
			}
			if item.Summary == "" {
				item.Summary = primaryGoal(body)
			}
		}
	}
	if item.Title == "" {
		item.Title = filepath.Base(filepath.Dir(festYAML))
	}
	return ""
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
	Title string `yaml:"title"`
	Ref   string `yaml:"ref"`
}

func fillWorkitem(item *Item, marker, readme string) string {
	var meta workitemMeta
	if err := readYAML(marker, &meta); err != nil {
		return err.Error()
	}
	if meta.Title != "" {
		item.Title = meta.Title
	}
	if meta.Ref != "" {
		item.ID = meta.Ref
	}
	if fileExists(readme) {
		text, err := readLimited(readme)
		if err != nil {
			return err.Error()
		}
		_, body := splitFrontmatter(text)
		item.Summary = firstParagraph(body)
	}
	return ""
}

func fillMarkdown(item *Item, path string) {
	text, err := readLimited(path)
	if err != nil {
		item.Kind = KindMarkdown
		item.Warning = err.Error()
		return
	}
	fm, body := splitFrontmatter(text)
	if len(fm) == 0 && item.Kind != KindFestival && item.Kind != KindWorkitem {
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

func parseStamp(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	stamp, err := time.Parse(time.RFC3339, value)
	if err != nil {
		stamp, err = time.Parse(time.RFC3339Nano, value)
	}
	if err != nil {
		return "", false
	}
	return stamp.Format("2006-01-02"), true
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

func splitFrontmatter(text string) (map[string]string, string) {
	if !strings.HasPrefix(text, "---\n") && !strings.HasPrefix(text, "---\r\n") {
		return nil, text
	}
	rest := text[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, text
	}
	raw := rest[:end]
	body := rest[end+4:]
	body = strings.TrimPrefix(body, "\n")
	var decoded map[string]any
	if err := yaml.Unmarshal([]byte(raw), &decoded); err != nil || decoded == nil {
		return nil, text
	}
	fm := make(map[string]string, len(decoded))
	for key, value := range decoded {
		switch typed := value.(type) {
		case string:
			fm[key] = typed
		case time.Time:
			fm[key] = typed.Format(time.RFC3339)
		default:
			fm[key] = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(fmtScalar(typed), "\n", " "), "\t", " "))
		}
	}
	return fm, body
}

func fmtScalar(value any) string {
	return strings.TrimSpace(yamlScalar(value))
}

func yamlScalar(value any) string {
	data, err := yaml.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func readYAML(path string, dest any) error {
	data, err := readLimited(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal([]byte(data), dest)
}

func readLimited(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, metaReadLimit)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return "", err
	}
	return string(bytes.TrimSpace(buf[:n])), nil
}

func fileExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}
