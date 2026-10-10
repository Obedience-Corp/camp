package explore

import (
	"encoding/base64"
	"os"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const (
	ProtocolAuto  = "auto"
	ProtocolKitty = "kitty"
	ProtocolITerm = "iterm"
	ProtocolOff   = "off"
)

// ChooseProtocol picks how a replay is drawn. Plain mode always returns off.
func ChooseProtocol(flag string, getenv func(string) string, plain bool) (string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	flag = strings.ToLower(strings.TrimSpace(flag))
	if flag == "" {
		flag = ProtocolAuto
	}
	switch flag {
	case ProtocolAuto, ProtocolKitty, ProtocolITerm, ProtocolOff:
	default:
		return "", camperrors.Newf("unknown --images %q; use auto, kitty, iterm, or off", flag)
	}
	if plain || flag == ProtocolOff {
		return ProtocolOff, nil
	}
	if flag == ProtocolKitty || flag == ProtocolITerm {
		return flag, nil
	}
	if getenv("KITTY_WINDOW_ID") != "" || strings.HasPrefix(getenv("TERM"), "xterm-ghostty") || getenv("WEZTERM_PANE") != "" {
		return ProtocolKitty, nil
	}
	if getenv("TERM_PROGRAM") == "iTerm.app" || getenv("LC_TERMINAL") == "iTerm2" {
		return ProtocolITerm, nil
	}
	return ProtocolOff, nil
}

// KittyPNG transmits png with the Kitty graphics protocol, displayed at the
// cursor. C=1 keeps the cursor where it was so the next row lands in place.
func KittyPNG(id int, png []byte) string {
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
			b.WriteString(",C=1,m=")
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

// ITermPNG transmits png with the iTerm2 inline image protocol.
func ITermPNG(png []byte, cols, rows int) string {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return "\033]1337;File=inline=1;preserveAspectRatio=1;width=" + itoa(cols) + ";height=" + itoa(rows) + ":" + base64.StdEncoding.EncodeToString(png) + "\a"
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
