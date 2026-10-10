package dungeon

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestExploreUnicodeAndLongTitles(t *testing.T) {
	text := strings.Repeat("é界👩‍💻é", 80)
	lines := wrapPlain(text, 23)
	if strings.Join(lines, "") != text {
		t.Fatal("wrapping lost text")
	}
	for _, line := range lines {
		if !utf8.ValidString(line) || lipgloss.Width(line) > 23 {
			t.Fatalf("invalid wrapped line %q", line)
		}
	}
	if got := statusTitle("ébauche"); got != "Ébauche" {
		t.Fatalf("status = %q", got)
	}
	if got := fit(strings.Repeat("界", 256<<10), 24); !strings.HasSuffix(got, "…") || lipgloss.Width(got) > 24 {
		t.Fatalf("truncation = %q", got)
	}
}

func BenchmarkExploreLongTitle(b *testing.B) {
	title := strings.Repeat("a", 256<<10)
	for b.Loop() {
		_ = fit(title, 48)
	}
}

func TestExploreAutoplayStartsAtFirstFrame(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		m := replayModel(explore.ProtocolKitty)
		m.reduced = reduced
		msg := explorePoster{gen: m.loadGen, item: replayItemPath, poster: []byte("last"), frames: [][]byte{[]byte("first"), []byte("last")}, delays: []time.Duration{time.Second, time.Second}}
		m, _ = press(t, m, msg)
		want := "first"
		if reduced {
			want = "last"
		}
		if string(m.poster) != want {
			t.Fatalf("reduced=%v: displayed %q", reduced, m.poster)
		}
	}
}

// Cancel when PNG starts inspecting a frame, after GIF decoding has finished.
type cancellingImage struct {
	image.Image
	cancel context.CancelFunc
}

func (im cancellingImage) Bounds() image.Rectangle { im.cancel(); return im.Image.Bounds() }
func TestExploreCancelsPNGEncoding(t *testing.T) {
	for _, point := range []string{"before-poster", "between-frames"} {
		t.Run(point, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			m := replayModel(explore.ProtocolKitty)
			frame := image.NewRGBA(image.Rect(0, 0, 2, 2))
			frame.Set(0, 0, color.White)
			m.decode = func(context.Context, string, int, int) (explore.Frames, error) {
				if point == "before-poster" {
					cancel()
				}
				return explore.Frames{Poster: frame, Frames: []image.Image{cancellingImage{frame, cancel}, nil}, Delays: []time.Duration{time.Second, time.Second}}, nil
			}
			msg := m.decodeCmd(ctx)().(explorePoster)
			if !errors.Is(msg.err, context.Canceled) || msg.frames != nil {
				t.Fatalf("cancelled encoding retained frames or succeeded: %+v", msg)
			}
		})
	}
}

func TestExploreReplayPaintsAfterBody(t *testing.T) {
	for _, protocol := range []string{explore.ProtocolKitty, explore.ProtocolITerm} {
		for _, width := range []int{64, 88, 120} {
			m := replayModel(protocol)
			m.width = width
			view := m.View()
			last := view[strings.LastIndex(view, "\n")+1:]
			if !strings.Contains(last, "\x1b7\x1b[") || !strings.HasSuffix(last, "\x1b8") {
				t.Fatalf("image not painted from footer (%s/%d)", protocol, width)
			}
			if strings.Contains(view[:strings.LastIndex(view, "\n")], "File=inline") || strings.Contains(view[:strings.LastIndex(view, "\n")], "a=T") {
				t.Fatal("image painted before body clearing")
			}
			if width == 88 && protocol == explore.ProtocolKitty && !strings.Contains(last, ",c=44,r=12,") {
				t.Fatal("Kitty placement exceeds right pane")
			}
		}
	}
}

func TestExploreStylesAndPlain(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	m := replayModel(explore.ProtocolOff)
	m.styles = newExploreStyles(false)
	styled := m.View()
	if !strings.Contains(styled, "\x1b[") || !strings.Contains(styled, "48;") {
		t.Fatal("missing color or selection background")
	}
	m.styles = newExploreStyles(true)
	plain := m.View()
	if bytes.ContainsAny([]byte(plain), "\x1b") {
		t.Fatal("plain view contains ANSI")
	}
	for _, line := range strings.Split(plain, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Fatal("view overflows")
		}
	}
}

// Exercise Bubble Tea's real line clearing, not just the model's View string.
type replayRenderProbe struct{ view string }

func (p replayRenderProbe) Init() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tea.QuitMsg{} })
}
func (p replayRenderProbe) Update(tea.Msg) (tea.Model, tea.Cmd) { return p, nil }
func (p replayRenderProbe) View() string                        { return p.view }
func TestExploreReplaySurvivesRendererLineClearing(t *testing.T) {
	for _, protocol := range []string{explore.ProtocolKitty, explore.ProtocolITerm} {
		m := replayModel(protocol)
		var out bytes.Buffer
		program := tea.NewProgram(replayRenderProbe{m.View()}, tea.WithInput(bytes.NewReader(nil)), tea.WithOutput(&out), tea.WithAltScreen(), tea.WithoutSignalHandler())
		if _, err := program.Run(); err != nil {
			t.Fatal(err)
		}
		output := out.String()
		marker := "\x1b]1337;File="
		if protocol == explore.ProtocolKitty {
			marker = "\x1b_Ga=T"
		}
		at := strings.Index(output, marker)
		help := strings.Index(output, "q quit")
		if at < 0 || help < 0 || at < help {
			t.Fatalf("image must follow the painted body and help: %q", output)
		}
		restore := strings.Index(output[at:], "\x1b8")
		erase := strings.Index(output[at:], "\x1b[K")
		if restore < 0 || (erase >= 0 && erase < restore) {
			t.Fatal("renderer erases image cells before restoring footer cursor")
		}
	}
}
