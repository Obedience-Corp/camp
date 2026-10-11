package dungeon

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

// checkLayout holds every view to the screen it is drawn on: exactly height
// rows, no row wider than the terminal, the status and help rows last, and
// the replay inside the body with no text beneath it.
func checkLayout(m exploreModel) []string {
	if m.height < 4 || m.width < 20 {
		return nil
	}
	var problems []string
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		return []string{fmt.Sprintf("%d rows, want %d", len(lines), m.height)}
	}
	for i, line := range lines {
		if w := lipgloss.Width(viewText(line)); w > m.width {
			problems = append(problems, fmt.Sprintf("row %d is %d columns wide: %q", i+1, w, viewText(line)))
		}
	}
	help := m.helpLines()
	lastBody := m.height - len(help)
	for i, want := range help {
		if got := lines[lastBody+i]; viewText(got) != viewText(want) {
			problems = append(problems, fmt.Sprintf("row %d = %q, want the help row %q", lastBody+i+1, viewText(got), viewText(want)))
		}
	}
	if status := m.shownStatus(); status != "" {
		if got := viewText(lines[lastBody-1]); got != fit(status, m.width) {
			problems = append(problems, fmt.Sprintf("row %d = %q, want the status %q", lastBody, got, status))
		}
		lastBody--
	}
	firstBody := 2 + len(m.noticeLines())
	if firstBody > lastBody {
		problems = append(problems, fmt.Sprintf("no body row: first %d, last %d", firstBody, lastBody))
	}
	row, col, cols, rows, ok := imageBox(m.View())
	if !ok {
		return problems
	}
	if row < firstBody || row+rows-1 > lastBody {
		problems = append(problems, fmt.Sprintf("replay rows %d-%d leave the body rows %d-%d", row, row+rows-1, firstBody, lastBody))
	}
	if col+cols-1 > m.width {
		problems = append(problems, fmt.Sprintf("replay columns %d-%d leave the %d-column terminal", col, col+cols-1, m.width))
	}
	for r := row; r < row+rows && r <= len(lines); r++ {
		text := strings.TrimRight(viewText(lines[r-1]), " ")
		if w := lipgloss.Width(text); w >= col {
			problems = append(problems, fmt.Sprintf("row %d has text under the replay at column %d: %q", r, col, text))
		}
	}
	return problems
}

func TestExploreLayoutMatrix(t *testing.T) {
	sizes := [][2]int{
		{20, 4}, {20, 5}, {21, 6}, {40, 10}, {59, 22}, {60, 21}, {60, 22}, {80, 22}, {80, 24},
		{87, 30}, {88, 15}, {88, 16}, {100, 29}, {100, 30}, {120, 40}, {200, 60},
	}
	doc := readerBody{markdown: true, text: strings.Repeat("# Heading\n\nSome **markdown** that is long enough to wrap on a narrow terminal.\n\n", 30)}
	seen := map[string]bool{}
	report := func(label string, m exploreModel) {
		for _, problem := range checkLayout(m) {
			// One line per kind of problem and mode keeps a broken layout readable.
			key := label[strings.LastIndex(label, " ")+1:] + ": " + problem
			if len(key) > 60 {
				key = key[:60]
			}
			if !seen[key] {
				seen[key] = true
				t.Errorf("%s: %s", label, problem)
			}
		}
	}
	for _, size := range sizes {
		for _, protocol := range []string{explore.ProtocolOff, explore.ProtocolKitty, explore.ProtocolITerm} {
			for _, n := range []int{0, 1, 3, 40} {
				for _, notices := range []int{0, 1, 2} {
					for _, status := range []string{"", "copied"} {
						base := screenFeed(t, size[0], size[1], protocol, n)
						if notices >= 1 {
							base.index.Warnings = []string{"festivals/.dungeon/archived: permission denied"}
						}
						if notices >= 2 {
							for i := range base.visible.Items {
								base.visible.Items[i].Warning = "fest.yaml: mapping values are not allowed"
							}
						}
						base.status = status
						cursors := []int{0}
						if rows := len(base.visible.Items); rows > 1 {
							cursors = []int{0, rows / 2, rows - 1}
						}
						for _, cursor := range cursors {
							m := base
							m.cursor = cursor
							if item, ok := m.focused(); ok && item.Replay != "" {
								w, h := m.stagePixels()
								m.poster, m.mediaFor = posterFor(item.Path, w, h), item.Path
							}
							label := fmt.Sprintf("%dx%d %s rows=%d notices=%d status=%q cursor=%d", size[0], size[1], protocol, n, notices, status, cursor)
							report(label+" feed", m)

							help := m
							help.help = true
							report(label+" help", help)

							filter := m
							filter.filtering = true
							filter.filter = strings.Repeat("query ", 30)
							report(label+" filter", filter)

							loading := m
							loading.loading = true
							report(label+" loading", loading)

							if _, ok := m.focused(); !ok {
								continue
							}
							reader := m
							reader.reading = true
							reader.reader = exploreReader{title: "doc", item: m.visible.Items[cursor], body: doc}
							reader.reader.lines = doc.render(reader.contentWidth(), true)
							report(label+" reader", reader)
							reader.reader.offset = len(reader.reader.lines)
							report(label+" reader-end", reader)

							listing := m
							listing.reading = true
							listing.reader = exploreReader{title: "docs", listing: true, item: m.visible.Items[cursor]}
							for i := range 50 {
								listing.reader.entries = append(listing.reader.entries, readerEntry{name: fmt.Sprintf("a-long-document-name-that-keeps-going-and-going-%02d.md", i)})
							}
							listing.reader.entry = 30
							report(label+" listing", listing)
						}
					}
				}
			}
		}
		empty := exploreModel{width: size[0], height: size[1], protocol: explore.ProtocolOff, query: explore.Query{Status: "finished", Dungeon: explore.LensAll}}
		_ = empty.applyQuery()
		label := fmt.Sprintf("%dx%d", size[0], size[1])
		report(label+" no-dungeons", empty)
		empty.loading = true
		report(label+" first-load", empty)
		empty.loading, empty.loadFailed = false, true
		empty.setStatus(exploreLoadStatus, "open /camp: permission denied", true)
		report(label+" load-failed", empty)
	}
}
