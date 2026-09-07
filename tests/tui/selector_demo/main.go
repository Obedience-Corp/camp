// Selector demo binary for PTY verification of internal/tui/selector.
// Driven by tests/tui/selector_pty.py; not a user-facing command.
package main

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/tui/selector"
)

type wrap struct {
	selector.Model
}

func (w wrap) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			if it, ok := w.Selected(); ok {
				fmt.Fprintf(os.Stderr, "SELECTED:%s\n", it.ID)
			}
			return w, tea.Quit
		case "ctrl+c":
			return w, tea.Quit
		case "esc":
			if !w.Filtering() {
				fmt.Fprintf(os.Stderr, "CANCELLED\n")
				return w, tea.Quit
			}
		}
	}
	m, cmd := w.Model.Update(msg)
	w.Model = m
	return w, cmd
}

func main() {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	items := []selector.Item{
		{ID: "none", Label: "(none)", Pin: selector.PinTop},
		{ID: "alpha", Label: "alpha"},
		{ID: "beta", Label: "beta", Rank: old},
		{ID: "gamma", Label: "gamma", Rank: recent},
	}
	m := selector.New(items, selector.Options{
		Title:     "Select:",
		Help:      "↑/↓ move · type to filter · enter select · esc cancel",
		InitialID: "gamma",
	})
	p := tea.NewProgram(wrap{Model: m}, tea.WithOutput(os.Stdout), tea.WithInput(os.Stdin))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
