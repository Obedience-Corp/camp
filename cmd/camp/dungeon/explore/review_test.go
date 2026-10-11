package explore

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
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
		m := replayModel(intexplore.ProtocolKitty)
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
			m := replayModel(intexplore.ProtocolKitty)
			frame := image.NewRGBA(image.Rect(0, 0, 2, 2))
			frame.Set(0, 0, color.White)
			m.decode = func(context.Context, string, int, int) (intexplore.Frames, error) {
				if point == "before-poster" {
					cancel()
				}
				return intexplore.Frames{Poster: frame, Frames: []image.Image{cancellingImage{frame, cancel}, nil}, Delays: []time.Duration{time.Second, time.Second}}, nil
			}
			msg := m.decodeCmd(ctx)().(explorePoster)
			if !errors.Is(msg.err, context.Canceled) || msg.frames != nil {
				t.Fatalf("cancelled encoding retained frames or succeeded: %+v", msg)
			}
		})
	}
}

func TestExploreKittyPlacementKeepsAspect(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
		want string
	}{
		{name: "square", w: 192, h: 192, want: ",c=24,r=12,"},
		{name: "portrait", w: 96, h: 192, want: ",c=12,r=12,"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := replayModel(intexplore.ProtocolKitty)
			png, err := intexplore.EncodePNG(image.NewRGBA(image.Rect(0, 0, tc.w, tc.h)))
			if err != nil {
				t.Fatal(err)
			}
			m.poster = png
			view := m.View()
			if strings.Contains(view, ",c=48,r=12,") {
				t.Fatal("replay stretched to the full stage")
			}
			if !strings.Contains(view, tc.want) {
				at := strings.Index(view, ",c=")
				got := view
				if at >= 0 && at+16 <= len(view) {
					got = view[at : at+16]
				}
				t.Fatalf("placement %q, want %s", got, tc.want)
			}
		})
	}
}

func TestExploreReplayStaysInsideVisibleBody(t *testing.T) {
	png, err := intexplore.EncodePNG(image.NewRGBA(image.Rect(0, 0, 192, 192)))
	if err != nil {
		t.Fatal(err)
	}
	items := make([]intexplore.Item, 12)
	for i := range items {
		day := time.Date(2026, time.January, i+1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		items[i] = intexplore.Item{
			DoneDate: day, DateSource: intexplore.DateBucket, Status: "completed",
			DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: intexplore.KindFestival,
			Title: "Item " + day, Summary: "A finished summary",
			Path: "festivals/.dungeon/completed/" + day + "/item", IsDir: true,
		}
	}
	const focus = "festivals/.dungeon/completed/2026-01-07/item"
	m := replayModel(intexplore.ProtocolKitty)
	m.width = 80
	m.height = 22
	m.status = "Copied the path"
	m.poster = png
	m.mediaFor = focus
	m.index.Items = items
	m.index.Warnings = []string{"festivals/.dungeon/archived: permission denied"}
	if err := m.applyQuery(); err != nil {
		t.Fatal(err)
	}
	m.cursor = -1
	for i, item := range m.visible.Items {
		if item.Path == focus {
			m.cursor = i
		}
	}
	if m.cursor < 0 {
		t.Fatal("focused item missing from the feed")
	}

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 22 {
		t.Fatalf("view lines = %d, want 22", len(lines))
	}
	status := lines[20]
	if !strings.Contains(status, "Copied the path") || strings.Contains(status, "a=T") {
		t.Fatalf("status row = %q", status)
	}
	if !strings.Contains(lines[21], "q quit") {
		t.Fatalf("help row = %q", lines[21])
	}
	// The feed window leaves room for the focused block, so the replay keeps
	// all eight rows and still ends above the status row.
	row, _, cols, rows, ok := imageBox(view)
	if !ok || cols != 16 || rows != 8 {
		t.Fatalf("placement %dx%d at row %d (ok %v), want 16x8", cols, rows, row, ok)
	}
	if cup, ok := replayCupRow(view); !ok || cup != row {
		t.Fatalf("image row = %d, ok %v, want %d", cup, ok, row)
	}
	if row+rows-1 >= 21 {
		t.Fatalf("replay rows %d-%d cover the status row", row, row+rows-1)
	}
	if problems := checkLayout(m); len(problems) > 0 {
		t.Fatal(problems)
	}

	// Whatever the window does, a reservation that reaches the end of the
	// body is painted only as far as the body goes.
	seq := m.imageSequence(m.width)
	body := make([]string, 22)
	body[17] = seq // rows 18, 19 and 20 are the last three of the body
	clipped := m.placeReplay(strings.Join(body, "\n"))
	if !strings.Contains(clipped, ",c=6,r=3,") || strings.Contains(clipped, ",r=8,") {
		at := strings.Index(clipped, ",c=")
		t.Fatalf("clipped placement %q, want ,c=6,r=3,", clipped[at:min(at+16, len(clipped))])
	}
	if cup, ok := replayCupRow(clipped); !ok || cup != 18 {
		t.Fatalf("clipped image row = %d, ok %v, want 18", cup, ok)
	}
}

func TestExploreITermRowsRepaintWhenTheImageChanges(t *testing.T) {
	mark := regexp.MustCompile(`\x1b\[\d+G\x1b\[\d+X`)
	m := replayModel(intexplore.ProtocolITerm)
	if !m.wide() {
		t.Fatal("fixture is not the wide layout")
	}
	before := strings.Split(m.View(), "\n")
	var under []int
	for i, line := range before {
		if mark.MatchString(line) {
			under = append(under, i)
		}
	}
	if len(under) < 2 || !strings.Contains(strings.Join(before, "\n"), "1337;File=") {
		t.Fatalf("replay was not placed over marked rows: %v", under)
	}
	if again := strings.Split(m.View(), "\n"); !slices.Equal(again, before) {
		t.Fatal("an unchanged image changed the frame, so it would flicker")
	}

	moved, _ := press(t, m, runes("j"))
	view := moved.View()
	if moved.poster != nil || strings.Contains(view, "1337;File=") || mark.MatchString(view) {
		t.Fatal("item without a replay still drew or reserved an image")
	}
	after := strings.Split(view, "\n")
	for _, i := range under {
		if i >= len(after) || after[i] == before[i] {
			t.Fatalf("row %d is unchanged, so Bubble Tea would leave the iTerm image on it", i+1)
		}
	}
	if !strings.Contains(view, "Add blog sections") {
		t.Fatal("the newly focused item is missing")
	}

	// A different placement has a different mark, so a replay that moves or
	// shrinks also repaints the rows it shares with the last one.
	if a, b := markReplayRows("x", 1, 1, 41, 48, ""), markReplayRows("x", 1, 1, 41, 14, ""); a == b {
		t.Fatal("a narrower placement left the row identical")
	}
	if a, b := markReplayRows("x", 1, 1, 41, 48, ""), markReplayRows("x", 1, 1, 51, 48, ""); a == b {
		t.Fatal("a shifted placement left the row identical")
	}
}

func replayCupRow(view string) (int, bool) {
	const mark = "\x1b7\x1b["
	at := strings.LastIndex(view, mark)
	if at < 0 {
		return 0, false
	}
	rest := view[at+len(mark):]
	semi := strings.IndexByte(rest, ';')
	if semi <= 0 {
		return 0, false
	}
	row := 0
	for _, r := range rest[:semi] {
		if r < '0' || r > '9' {
			return 0, false
		}
		row = row*10 + int(r-'0')
	}
	return row, row > 0
}

func TestExploreReplayPaintsAfterBody(t *testing.T) {
	for _, protocol := range []string{intexplore.ProtocolKitty, intexplore.ProtocolITerm} {
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
			if width == 88 && protocol == intexplore.ProtocolKitty && !strings.Contains(last, ",c=44,r=12,") {
				t.Fatal("Kitty placement exceeds right pane")
			}
		}
	}
}

func TestExploreStylesAndPlain(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	m := replayModel(intexplore.ProtocolOff)
	m.styles = newExploreStyles(false, theme.ThemeAdaptive)
	styled := m.View()
	if !strings.Contains(styled, "\x1b[") || !strings.Contains(styled, "48;") {
		t.Fatal("missing color or selection background")
	}
	m.styles = newExploreStyles(true, theme.ThemeAdaptive)
	// Plain output carries no styling. Placing the right pane at its column
	// is cursor control, which plain mode keeps.
	plain := viewPanePlacement.ReplaceAllString(m.View(), "")
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
	for _, protocol := range []string{intexplore.ProtocolKitty, intexplore.ProtocolITerm} {
		m := replayModel(protocol)
		var out bytes.Buffer
		program := tea.NewProgram(replayRenderProbe{m.View()}, tea.WithInput(bytes.NewReader(nil)), tea.WithOutput(&out), tea.WithAltScreen(), tea.WithoutSignalHandler())
		if _, err := program.Run(); err != nil {
			t.Fatal(err)
		}
		output := out.String()
		marker := "\x1b]1337;File="
		if protocol == intexplore.ProtocolKitty {
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
