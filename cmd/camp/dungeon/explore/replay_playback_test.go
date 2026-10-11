package explore

import (
	"context"
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

func TestExploreReplayIsAskedForOnceAndKept(t *testing.T) {
	ready := func(w, h int) exploreModel {
		m := screenFeed(t, w, h, intexplore.ProtocolKitty, 6)
		m.reduced = false
		m.decode = func(context.Context, string, int, int) (intexplore.Frames, error) {
			return intexplore.Frames{}, context.Canceled
		}
		return m
	}

	// The model starts at 80x24. A terminal of that size reports no change in
	// geometry, and the focused replay still has to be requested.
	m := ready(80, 24)
	m, cmd := press(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	item, _ := m.focused()
	if cmd == nil || m.posterFor != item.Path {
		t.Fatal("the first size message at 80x24 did not ask for the focused replay")
	}
	gen := m.loadGen
	if m, cmd = press(t, m, tea.WindowSizeMsg{Width: 80, Height: 24}); cmd != nil || m.loadGen != gen {
		t.Fatal("a repeated size message asked for the replay again")
	}

	// A failed refresh keeps the cached feed; its focused replay still draws.
	failed := ready(80, 24)
	failed, _ = press(t, failed, exploreLoaded{gen: failed.indexGen, err: context.DeadlineExceeded})
	if failed.loadGen == 0 || failed.posterFor != item.Path || !failed.statusRow().failed {
		t.Fatalf("a failed refresh did not ask for the focused replay (status %q)", failed.shownStatus())
	}

	// Drawn and playing: a refresh that leaves the row alone must not blank
	// the replay and start it over.
	playing := ready(120, 40)
	playing, _ = press(t, playing, tea.WindowSizeMsg{Width: 120, Height: 40})
	poster := posterFor(item.Path, 64, 64)
	playing, _ = press(t, playing, explorePoster{
		gen: playing.loadGen, item: item.Path, poster: poster,
		frames: [][]byte{poster, poster}, delays: []time.Duration{time.Second, time.Second},
	})
	if !playing.playing || playing.poster == nil {
		t.Fatal("fixture: the replay is not playing")
	}
	for name, changed := range map[string]bool{"unchanged refresh": false, "refresh with other rows changed": true} {
		idx := playing.index
		if changed {
			idx.Items = append([]intexplore.Item(nil), idx.Items...)
			idx.Items[len(idx.Items)-1].Title = "renamed elsewhere"
		}
		after, _ := press(t, playing, exploreLoaded{gen: playing.indexGen, index: idx, changed: changed})
		if after.poster == nil || !after.playing || after.loadGen != playing.loadGen || after.tickGen != playing.tickGen {
			t.Errorf("%s restarted a replay whose row did not change", name)
		}
	}
	// Still decoding: the refresh must not cancel and restart it either.
	pending := ready(120, 40)
	pending, _ = press(t, pending, tea.WindowSizeMsg{Width: 120, Height: 40})
	if after, _ := press(t, pending, exploreLoaded{gen: pending.indexGen, index: pending.index}); after.loadGen != pending.loadGen || after.posterFor != item.Path {
		t.Error("an unchanged refresh restarted a decode that was still running")
	}

	// A different replay file for the focused row is a different replay.
	idx := playing.index
	idx.Items = append([]intexplore.Item(nil), idx.Items...)
	idx.Items[0].Replay = idx.Items[0].Path + "/second-replay.gif"
	swapped, _ := press(t, playing, exploreLoaded{gen: playing.indexGen, index: idx, changed: true})
	if swapped.loadGen == playing.loadGen || swapped.poster != nil || swapped.posterReplay != idx.Items[0].Replay {
		t.Error("a new replay file for the focused row was not decoded")
	}

	// r asks for the camp to be read again, the focused replay included.
	rescan, _ := press(t, playing, runes("r"))
	rescan, _ = press(t, rescan, exploreLoaded{gen: rescan.indexGen, index: playing.index, changed: true})
	if rescan.loadGen == playing.loadGen || rescan.posterFor != item.Path {
		t.Error("an explicit rescan did not decode the focused replay again")
	}
}

func TestExploreResizeSchedulesSkippedReplay(t *testing.T) {
	m := replayModel(intexplore.ProtocolKitty)
	m.width, m.height = 50, 12
	m.poster, m.mediaFor = nil, ""
	m, _ = press(t, m, tea.WindowSizeMsg{Width: 50, Height: 12})
	gen := m.loadGen
	m, cmd := press(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd == nil || m.loadGen == gen {
		t.Fatal("enlarging the terminal did not schedule the replay it skipped")
	}
}

func TestExploreFocusChangeCancelsReplayDecode(t *testing.T) {
	m := replayModel(intexplore.ProtocolKitty)
	m.index.Items[1].Replay = m.index.Items[1].Path + "/replay.gif"
	if err := m.applyQuery(); err != nil {
		t.Fatal(err)
	}
	started := make(chan context.Context, 4)
	m.decode = func(ctx context.Context, _ string, _, _ int) (intexplore.Frames, error) {
		started <- ctx
		<-ctx.Done()
		return intexplore.Frames{}, ctx.Err()
	}
	run := func(cmd tea.Cmd) <-chan tea.Msg {
		out := make(chan tea.Msg, 1)
		go func() { out <- cmd() }()
		return out
	}
	await := func(what string) context.Context {
		select {
		case ctx := <-started:
			return ctx
		case <-time.After(2 * time.Second):
			t.Fatalf("%s decode never started", what)
			return nil
		}
	}

	m, cmd := press(t, m, exploreDecode{gen: m.loadGen})
	if cmd == nil {
		t.Fatal("no decode for the focused replay")
	}
	firstDone := run(cmd)
	first := await("first")

	m, _ = press(t, m, runes("j"))
	if !errors.Is(first.Err(), context.Canceled) {
		t.Fatal("moving focus left the first decode running")
	}
	select {
	case msg := <-firstDone:
		m, _ = press(t, m, msg)
	case <-time.After(2 * time.Second):
		t.Fatal("the cancelled decode did not return")
	}
	if row := m.statusRow(); row.failed {
		t.Fatalf("a cancelled decode reported %q", row.text)
	}

	m, cmd = press(t, m, exploreDecode{gen: m.loadGen})
	secondDone := run(cmd)
	second := await("second")
	if second.Err() != nil {
		t.Fatal("the new focus started with a cancelled decode")
	}
	m, _ = press(t, m, runes("q"))
	if !errors.Is(second.Err(), context.Canceled) {
		t.Fatal("quitting left the decode running")
	}
	<-secondDone
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

// A replay file that ends early shows the still its whole frames make and
// says why it does not play, instead of claiming it is too long.
func TestExploreSaysWhenAReplayFileIsIncomplete(t *testing.T) {
	const incomplete = "The replay file is incomplete. Showing a still."
	m := screenFeed(t, 100, 30, intexplore.ProtocolKitty, 6)
	item, _ := m.focused()
	m.decode = func(context.Context, string, int, int) (intexplore.Frames, error) {
		return intexplore.Frames{Poster: image.NewRGBA(image.Rect(0, 0, 8, 8))}, intexplore.ErrReplayIncomplete
	}
	cmd := m.decodeCmd(context.Background())
	if cmd == nil {
		t.Fatal("the focused row has no replay to decode")
	}
	msg, ok := cmd().(explorePoster)
	if !ok || !msg.cut || msg.tooLong || msg.err != nil || msg.poster == nil {
		t.Fatalf("decode reported %+v, want a still marked incomplete", msg)
	}
	m.posterFor, m.posterReplay = item.Path, item.Replay
	m, _ = press(t, m, msg)
	view := m.View()
	if !strings.Contains(view, incomplete) || strings.Contains(view, "too long") {
		t.Fatalf("feed does not explain the incomplete replay:\n%s", view)
	}
	if m.poster == nil || m.playing {
		t.Fatalf("incomplete replay: poster=%v playing=%v", m.poster != nil, m.playing)
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

func TestExplorePauseInvalidatesPendingTick(t *testing.T) {
	m := replayModel(intexplore.ProtocolKitty)
	m.frames = [][]byte{{1}, {2}, {3}}
	m.delays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	m.playing = true
	pending := m.tick()()
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	m, resumed := press(t, m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	if !m.playing || resumed == nil {
		t.Fatal("space did not resume playback")
	}
	frame := m.frame
	m, cmd := press(t, m, pending)
	if cmd != nil || m.frame != frame {
		t.Fatal("a tick scheduled before the pause still advanced playback")
	}
}
