//go:build dev

package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/nav/fuzzy"
)

// maxPickerRows bounds how many workitem rows render at once. The "none" row is
// always shown; matches beyond the window scroll into view around the cursor.
const maxPickerRows = 8

// WorkitemChoice is one selectable workitem in the create-flow picker. Path is
// the campaign-relative value written to the quest as a Link (identical to what
// `camp quest link` stores), so the binding renders through the existing
// quest show / quest links surfaces unchanged.
type WorkitemChoice struct {
	Path  string
	Title string
	Ref   string
	Type  string
}

func (c WorkitemChoice) label() string {
	title := strings.TrimSpace(c.Title)
	if title == "" {
		title = c.Path
	}
	var b strings.Builder
	b.WriteString(title)
	if c.Ref != "" {
		b.WriteString(" (")
		b.WriteString(c.Ref)
		b.WriteString(")")
	}
	if c.Type != "" {
		b.WriteString(" [")
		b.WriteString(c.Type)
		b.WriteString("]")
	}
	return b.String()
}

func (c WorkitemChoice) searchText() string {
	return c.Title + " " + c.Ref + " " + c.Type + " " + c.Path
}

// workitemPicker is a filterable list step for optionally binding a workitem at
// quest-create time. Row 0 is always a "none / skip" row so the fast path is a
// single Enter; typing narrows the list with fuzzy matching.
type workitemPicker struct {
	choices []WorkitemChoice
	filter  textinput.Model

	// visible holds indices into choices after the current filter, ordered by
	// fuzzy score. cursor 0 is the skip row; cursor 1..len(visible) selects
	// choices[visible[cursor-1]].
	visible []int
	cursor  int

	done     bool
	selected string
}

// updateWorkitem drives the optional binding step. Esc skips (finish with no
// binding); Ctrl+C cancels the whole flow; everything else is the picker's own
// filter/navigation/select handling. The result struct is already populated by
// finish(), so this only records the chosen path.
func (m QuestCreateModel) updateWorkitem(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.cancelled = true
		m.result = nil
		m.step = createStepDone
		return m, tea.Quit
	case "esc":
		m.result.WorkitemPath = ""
		m.step = createStepDone
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.picker, cmd = m.picker.update(msg)
	if m.picker.done {
		m.result.WorkitemPath = m.picker.selected
		m.step = createStepDone
		return m, tea.Quit
	}
	return m, cmd
}

func newWorkitemPicker(choices []WorkitemChoice) workitemPicker {
	fi := textinput.New()
	fi.Placeholder = "Type to filter by name, id, or festival"
	fi.CharLimit = 120
	fi.Width = 50
	fi.Focus()

	p := workitemPicker{choices: choices, filter: fi}
	p.recompute()
	return p
}

// active reports whether the picker has any candidates to offer.
func (p workitemPicker) active() bool {
	return len(p.choices) > 0
}

func (p *workitemPicker) setWidth(width int) {
	p.filter.Width = min(max(width-10, 20), 80)
}

// update handles filter/navigation keys. Enter, Esc, and Ctrl+C are owned by the
// parent model (select vs. skip vs. cancel), so they are not handled here.
func (p workitemPicker) update(msg tea.KeyMsg) (workitemPicker, tea.Cmd) {
	switch msg.String() {
	case "enter":
		p.done = true
		if p.cursor > 0 && p.cursor-1 < len(p.visible) {
			p.selected = p.choices[p.visible[p.cursor-1]].Path
		} else {
			p.selected = ""
		}
		return p, nil
	case "up", "ctrl+p":
		if p.cursor > 0 {
			p.cursor--
		}
		return p, nil
	case "down", "ctrl+n":
		if p.cursor < len(p.visible) {
			p.cursor++
		}
		return p, nil
	}

	prevQuery := p.filter.Value()
	var cmd tea.Cmd
	p.filter, cmd = p.filter.Update(msg)
	if p.filter.Value() != prevQuery {
		p.recompute()
	}
	return p, cmd
}

// recompute refilters the candidate list against the current query. When the
// query is non-empty and produces matches, the cursor lands on the top match so
// a single Enter selects it; an empty query keeps the cursor on the skip row.
func (p *workitemPicker) recompute() {
	query := strings.TrimSpace(p.filter.Value())
	if query == "" {
		p.visible = allIndices(len(p.choices))
		p.cursor = 0
		return
	}

	type scored struct {
		idx   int
		score int
	}
	var matches []scored
	for i, choice := range p.choices {
		if score, ok := fuzzyScore(query, choice.searchText()); ok {
			matches = append(matches, scored{idx: i, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	p.visible = make([]int, len(matches))
	for i, m := range matches {
		p.visible[i] = m.idx
	}
	if len(p.visible) > 0 {
		p.cursor = 1
	} else {
		p.cursor = 0
	}
}

func (p workitemPicker) view() string {
	var b strings.Builder
	b.WriteString(FieldLabelStyle.Render("Bind a workitem (optional):"))
	b.WriteString("\n")
	b.WriteString(p.filter.View())
	b.WriteString("\n\n")

	b.WriteString(p.renderRow(0, noneRowLabel))
	b.WriteString("\n")

	start, end := windowBounds(p.cursor, len(p.visible), maxPickerRows)
	if start > 0 {
		b.WriteString(HelpStyle.Render("  ↑ more"))
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		b.WriteString(p.renderRow(i+1, p.choices[p.visible[i]].label()))
		b.WriteString("\n")
	}
	if end < len(p.visible) {
		b.WriteString(HelpStyle.Render("  ↓ more"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(HelpStyle.Render("Type: filter • ↑↓: move • Enter: select • Esc: skip"))
	return b.String()
}

const noneRowLabel = "(none - skip binding)"

func (p workitemPicker) renderRow(cursorIndex int, text string) string {
	if p.cursor == cursorIndex {
		return SelectedStyle.Render("> " + text)
	}
	return FieldValueStyle.Render("  " + text)
}

func allIndices(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// fuzzyScore requires every whitespace-separated term to match as a subsequence
// of the target, summing per-term scores. Returns (0,false) when any term misses.
func fuzzyScore(query, target string) (int, bool) {
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return 0, true
	}
	total := 0
	for _, term := range terms {
		score, _ := fuzzy.Score(term, target)
		if score <= 0 {
			return 0, false
		}
		total += score
	}
	return total, true
}

// windowBounds returns the [start,end) slice of a list of length n that keeps
// the row at cursor (1-based over the list, 0 = skip row) visible within size.
func windowBounds(cursor, n, size int) (int, int) {
	if n <= size {
		return 0, n
	}
	// cursor is 1-based over the visible list; convert to 0-based list index.
	idx := cursor - 1
	if idx < 0 {
		idx = 0
	}
	start := idx - size/2
	if start < 0 {
		start = 0
	}
	end := start + size
	if end > n {
		end = n
		start = end - size
	}
	return start, end
}
