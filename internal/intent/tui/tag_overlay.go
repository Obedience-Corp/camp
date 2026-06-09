package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TagOverlay is a reusable tag picker: a toggle list over the configured tags
// plus a text input for a freeform custom tag. It is embedded by the capture
// flow and the explorer; both route key messages to Update while it is active.
type TagOverlay struct {
	order     []string        // display order (configured tags, then custom additions)
	selected  map[string]bool // which tags are toggled on
	cursor    int
	input     textinput.Model
	inputting bool
	done      bool
	cancelled bool
}

// NewTagOverlay builds an overlay offering available tags, with current already
// selected. Custom tags present in current but not in available are appended.
func NewTagOverlay(available, current []string) TagOverlay {
	selected := make(map[string]bool, len(current))
	for _, t := range current {
		selected[t] = true
	}

	order := make([]string, 0, len(available)+len(current))
	seen := make(map[string]bool)
	for _, t := range available {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		order = append(order, t)
	}
	for _, t := range current {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		order = append(order, t)
	}

	ti := textinput.New()
	ti.Placeholder = "custom tag"
	ti.CharLimit = 40
	ti.Width = 30

	return TagOverlay{order: order, selected: selected, input: ti}
}

// Update routes a key message. It returns the updated overlay and true once the
// overlay has finished (confirmed or cancelled).
func (o TagOverlay) Update(msg tea.KeyMsg) (TagOverlay, bool) {
	if o.inputting {
		switch msg.String() {
		case "enter":
			o.addCustomTag()
			return o, false
		case "esc":
			o.inputting = false
			o.input.Blur()
			o.input.SetValue("")
			return o, false
		default:
			var cmd tea.Cmd
			o.input, cmd = o.input.Update(msg)
			_ = cmd
			return o, false
		}
	}

	switch msg.String() {
	case "esc", "ctrl+c":
		o.cancelled = true
		o.done = true
		return o, true
	case "enter":
		o.done = true
		return o, true
	case "j", "down":
		if o.cursor < len(o.order)-1 {
			o.cursor++
		}
	case "k", "up":
		if o.cursor > 0 {
			o.cursor--
		}
	case " ":
		if o.cursor >= 0 && o.cursor < len(o.order) {
			tag := o.order[o.cursor]
			o.selected[tag] = !o.selected[tag]
		}
	case "i", "a", "/":
		o.inputting = true
		o.input.Focus()
	}
	return o, false
}

// addCustomTag adds the typed tag (selected) and exits input mode.
func (o *TagOverlay) addCustomTag() {
	tag := strings.TrimSpace(o.input.Value())
	o.input.SetValue("")
	o.input.Blur()
	o.inputting = false
	if tag == "" {
		return
	}
	if !o.selected[tag] {
		// Append to order only if not already present.
		known := false
		for _, t := range o.order {
			if t == tag {
				known = true
				break
			}
		}
		if !known {
			o.order = append(o.order, tag)
		}
	}
	o.selected[tag] = true
}

// Result returns the selected tags in a stable, sorted order.
func (o TagOverlay) Result() []string {
	tags := make([]string, 0, len(o.selected))
	for t, on := range o.selected {
		if on {
			tags = append(tags, t)
		}
	}
	sort.Strings(tags)
	return tags
}

// Cancelled reports whether the overlay was dismissed without confirming.
func (o TagOverlay) Cancelled() bool { return o.cancelled }

// Done reports whether the overlay has finished.
func (o TagOverlay) Done() bool { return o.done }

// View renders the overlay.
func (o TagOverlay) View() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render("Tags"))
	b.WriteString("\n\n")

	if len(o.order) == 0 {
		b.WriteString(HelpStyle.Render("No configured tags. Press i to add a custom tag.\n\n"))
	}

	for i, tag := range o.order {
		cursor := "  "
		if i == o.cursor {
			cursor = "> "
		}
		box := "[ ]"
		if o.selected[tag] {
			box = "[x]"
		}
		line := cursor + box + " " + tag
		if i == o.cursor {
			line = lipgloss.NewStyle().Foreground(pal.Accent).Render(line)
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	if o.inputting {
		b.WriteString("Custom tag: " + o.input.View() + "\n")
		b.WriteString(HelpStyle.Render("Enter: add . Esc: cancel input"))
	} else {
		b.WriteString(HelpStyle.Render("space: toggle . i: custom tag . Enter: confirm . Esc: cancel"))
	}

	return b.String()
}
