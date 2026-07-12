// Package bginit seeds lipgloss's background-color cache before any
// charmbracelet package init can query the terminal.
//
// bubbletea v1 calls lipgloss.HasDarkBackground() from a package init
// (tea_init.go). Without a cached value, lipgloss asks termenv, which writes
// OSC 11 / DSR queries to the tty and blocks up to termenv.OSCTimeout (5s)
// waiting for a reply. Interactive terminals answer instantly, but recorder,
// CI, and agent ptys that never answer freeze every camp command for the full
// timeout before a single byte of output appears.
//
// Seeding an explicit value makes lipgloss skip the terminal query entirely.
// This package must initialize before bubbletea, so it may only import
// lipgloss (never bubbletea or huh, directly or transitively), and cmd/camp
// must import it alongside the command subtree.
//
// The seed mirrors termenv's non-query fallback: the last field of COLORFGBG
// is the background ANSI color, and a missing or unparsable value means dark.
// Values 0..6 and 8 count as dark, 7 and 9..15 as light. ANSI 8 (bright black)
// intentionally diverges from termenv's luminance math and is treated as dark,
// because dark is the safer readability default when the terminal cannot be
// queried. An explicit theme choice refines this later through the theme
// package.
package bginit

import (
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func init() {
	lipgloss.SetHasDarkBackground(backgroundIsDark(os.Getenv("COLORFGBG")))
}

func backgroundIsDark(colorFGBG string) bool {
	if !strings.Contains(colorFGBG, ";") {
		return true
	}
	fields := strings.Split(colorFGBG, ";")
	bg, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil {
		return true
	}
	return bg >= 0 && bg <= 8 && bg != 7
}
