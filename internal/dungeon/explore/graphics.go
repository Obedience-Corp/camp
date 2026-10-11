package explore

import (
	"encoding/base64"
	"os"
	"strings"
)

const (
	ProtocolAuto  = "auto"
	ProtocolKitty = "kitty"
	ProtocolITerm = "iterm"
	ProtocolOff   = "off"
)

// ChooseProtocol picks how a replay is drawn. Plain mode always returns off,
// and so does a terminal multiplexer (insideMultiplexer says why). Auto also
// returns off inside an editor's terminal or any terminal it cannot name;
// naming a protocol forces it there.
func ChooseProtocol(flag string, getenv func(string) string, plain bool) (string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	flag = protocolName(flag)
	switch flag {
	case ProtocolAuto, ProtocolKitty, ProtocolITerm, ProtocolOff:
	default:
		return "", errUnknownImages(flag)
	}
	if plain || flag == ProtocolOff || insideMultiplexer(getenv) {
		return ProtocolOff, nil
	}
	if flag == ProtocolKitty || flag == ProtocolITerm {
		return flag, nil
	}
	if insideEditorTerminal(getenv) {
		return ProtocolOff, nil
	}
	return hostProtocol(getenv), nil
}

// NamedInMultiplexer reports an --images value that names a protocol inside
// a multiplexer, where ChooseProtocol turns it off.
func NamedInMultiplexer(flag string, getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch protocolName(flag) {
	case ProtocolKitty, ProtocolITerm:
		return insideMultiplexer(getenv)
	}
	return false
}

// protocolName is an --images value as ChooseProtocol compares it.
func protocolName(flag string) string {
	flag = strings.ToLower(strings.TrimSpace(flag))
	if flag == "" {
		return ProtocolAuto
	}
	return flag
}

// hostProtocol names the protocol of the terminal that draws the screen.
// KITTY_WINDOW_ID, WEZTERM_PANE, and LC_TERMINAL are inherited by whatever is
// started from that terminal, another terminal included, where they would
// reserve rows for a replay that never draws. TERM and TERM_PROGRAM are set by
// each terminal for itself, so they are read first, and a TERM_PROGRAM that
// names some other terminal turns replays off.
func hostProtocol(getenv func(string) string) string {
	term := getenv("TERM")
	if strings.HasPrefix(term, "xterm-kitty") || strings.HasPrefix(term, "xterm-ghostty") {
		return ProtocolKitty
	}
	if namesTerminalWithoutGraphics(term, getenv) {
		return ProtocolOff
	}
	switch getenv("TERM_PROGRAM") {
	case "iTerm.app":
		return ProtocolITerm
	case "WezTerm", "ghostty":
		return ProtocolKitty
	case "":
		// Kitty sets no TERM_PROGRAM, and none crosses an SSH hop.
	default:
		return ProtocolOff
	}
	if getenv("KITTY_WINDOW_ID") != "" || getenv("WEZTERM_PANE") != "" {
		return ProtocolKitty
	}
	if getenv("LC_TERMINAL") == "iTerm2" {
		return ProtocolITerm
	}
	return ProtocolOff
}

// namesTerminalWithoutGraphics reports a terminal that names itself and draws
// neither protocol: Alacritty, foot, and the Linux console in TERM, a
// JetBrains IDE's terminal in TERMINAL_EMULATOR. None of them sets
// TERM_PROGRAM, so one started from a shell in iTerm or Kitty still carries
// that terminal's TERM_PROGRAM, LC_TERMINAL, or KITTY_WINDOW_ID.
func namesTerminalWithoutGraphics(term string, getenv func(string) string) bool {
	for _, name := range []string{"alacritty", "foot", "linux", "dumb"} {
		if term == name || strings.HasPrefix(term, name+"-") {
			return true
		}
	}
	return getenv("TERMINAL_EMULATOR") == "JetBrains-JediTerm"
}

// insideMultiplexer reports tmux, GNU screen, or zellij. An image sequence
// written there does not reach the terminal that draws the screen, so the
// rows a replay is given stay blank. tmux also takes a Kitty sequence for the
// pane title, which its status line shows and which stays after the explorer
// exits. TERM is checked as well as the variables each one sets: unlike them
// it crosses an SSH hop made from inside one.
func insideMultiplexer(getenv func(string) string) bool {
	for _, name := range []string{"TMUX", "STY", "ZELLIJ"} {
		if getenv(name) != "" {
			return true
		}
	}
	term := getenv("TERM")
	return strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux")
}

// insideEditorTerminal reports a terminal run by Neovim, Vim, or Emacs. They
// keep the outer terminal's KITTY_WINDOW_ID and LC_TERMINAL but may not
// forward its image sequences, so detection would reserve rows for a replay
// that never draws.
func insideEditorTerminal(getenv func(string) string) bool {
	for _, name := range []string{"NVIM", "VIM_TERMINAL", "INSIDE_EMACS"} {
		if getenv(name) != "" {
			return true
		}
	}
	return false
}

// KittyPNG transmits png with the Kitty graphics protocol, displayed at the
// cursor. C=1 keeps the cursor where it was so the next row lands in place.
func KittyPNG(id int, png []byte, cols, rows int) string {
	payload := base64.StdEncoding.EncodeToString(png)
	const chunk = 4096
	var b strings.Builder
	first := true
	for len(payload) > 0 {
		n := min(chunk, len(payload))
		part := payload[:n]
		payload = payload[n:]
		more := 0
		if len(payload) > 0 {
			more = 1
		}
		if first {
			b.WriteString("\033_G")
			b.WriteString("a=T,f=100,q=2,i=")
			b.WriteString(itoa(id))
			b.WriteString(",C=1,c=" + itoa(max(1, cols)) + ",r=" + itoa(max(1, rows)) + ",m=")
			b.WriteString(itoa(more))
			b.WriteByte(';')
			b.WriteString(part)
			b.WriteString("\033\\")
			first = false
			continue
		}
		b.WriteString("\033_Gm=")
		b.WriteString(itoa(more))
		b.WriteByte(';')
		b.WriteString(part)
		b.WriteString("\033\\")
	}
	return b.String()
}

// HasKittyImage reports whether s places a Kitty image.
func HasKittyImage(s string) bool {
	return strings.Contains(s, "\033_Ga=T")
}

// KittyDelete removes the image id placed by KittyPNG.
func KittyDelete(id int) string {
	return "\033_Ga=d,d=i,q=2,i=" + itoa(id) + "\033\\"
}

// ITermPNG transmits png with the iTerm2 inline image protocol. name fills
// the protocol's file name field, which iTerm draws nowhere for an inline
// image; an empty name leaves the field out.
func ITermPNG(name string, png []byte, cols, rows int) string {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	args := "inline=1;preserveAspectRatio=1;width=" + itoa(cols) + ";height=" + itoa(rows)
	if name != "" {
		args = "name=" + base64.StdEncoding.EncodeToString([]byte(name)) + ";" + args
	}
	return "\033]1337;File=" + args + ":" + base64.StdEncoding.EncodeToString(png) + "\a"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [16]byte
	i := len(digits)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
