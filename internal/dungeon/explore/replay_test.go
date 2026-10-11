package explore

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"math"
	"os"
	"testing"
)

func TestDecodeReplayCompositesPartialFrames(t *testing.T) {
	pal := color.Palette{
		color.RGBA{0, 0, 0, 255},
		color.RGBA{255, 0, 0, 255},
		color.RGBA{0, 255, 0, 255},
	}
	first := image.NewPaletted(image.Rect(0, 0, 2, 2), pal)
	for y := range 2 {
		for x := range 2 {
			first.SetColorIndex(x, y, 1)
		}
	}
	second := image.NewPaletted(image.Rect(1, 1, 2, 2), pal)
	second.SetColorIndex(1, 1, 2)
	g := &gif.GIF{
		Image:    []*image.Paletted{first, second},
		Delay:    []int{1, 50},
		Disposal: []byte{gif.DisposalNone, gif.DisposalNone},
		Config:   image.Config{ColorModel: pal, Width: 2, Height: 2},
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	got, err := decodeReplay(context.Background(), buf.Bytes(), 8, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Frames) != 2 {
		t.Fatalf("frames = %d", len(got.Frames))
	}
	if r, g, b, _ := got.Poster.At(0, 0).RGBA(); r>>8 != 255 || g>>8 != 0 || b>>8 != 0 {
		t.Fatalf("poster origin = %d %d %d", r>>8, g>>8, b>>8)
	}
	if r, g, b, _ := got.Poster.At(1, 1).RGBA(); r>>8 != 0 || g>>8 != 255 || b>>8 != 0 {
		t.Fatalf("poster corner = %d %d %d, want green", r>>8, g>>8, b>>8)
	}
	if got.Delays[1] < minFrameDelay {
		t.Fatalf("delay = %s, want at least %s", got.Delays[1], minFrameDelay)
	}
}

func TestChooseProtocol(t *testing.T) {
	env := map[string]string{"TERM_PROGRAM": "iTerm.app"}
	got, err := ChooseProtocol("auto", func(k string) string { return env[k] }, false)
	if err != nil || got != ProtocolITerm {
		t.Fatalf("protocol = %q, %v", got, err)
	}
	got, err = ChooseProtocol("auto", func(k string) string { return env[k] }, true)
	if err != nil || got != ProtocolOff {
		t.Fatalf("plain protocol = %q, %v", got, err)
	}
	if _, err := ChooseProtocol("sixel", os.Getenv, false); err == nil {
		t.Fatal("unknown protocol error = nil")
	}
}

func TestKittyPNGRoundTripMarker(t *testing.T) {
	png := []byte{1, 2, 3, 4}
	seq := KittyPNG(7, png, 44, 12)
	if !bytes.Contains([]byte(seq), []byte("a=T,f=100,q=2,i=7")) {
		t.Fatalf("sequence = %q", seq)
	}
	if !bytes.Contains([]byte(seq), []byte("\033\\")) {
		t.Fatal("sequence missing kitty terminator")
	}
	del := KittyDelete(7)
	if del != "\033_Ga=d,d=i,q=2,i=7\033\\" {
		t.Fatalf("delete = %q", del)
	}
}

func encodeReplay(t *testing.T, frames []*image.Paletted, w, h int) []byte {
	t.Helper()
	g := &gif.GIF{Config: image.Config{Width: w, Height: h}}
	for _, frame := range frames {
		g.Image = append(g.Image, frame)
		g.Delay = append(g.Delay, 10)
		g.Disposal = append(g.Disposal, gif.DisposalNone)
	}
	g.Config.ColorModel = frames[0].Palette
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func solidFrame(w, h int, index uint8) *image.Paletted {
	pal := color.Palette{color.RGBA{0, 0, 0, 255}, color.RGBA{255, 0, 0, 255}}
	frame := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	if index != 0 {
		for i := range frame.Pix {
			frame.Pix[i] = index
		}
	}
	return frame
}

func TestDecodeReplayRejectsHugeCanvasBeforeDecoding(t *testing.T) {
	const side = 4096
	valid := encodeReplay(t, []*image.Paletted{solidFrame(side, side, 0)}, side, side)
	headerOnly := []byte("GIF89a")
	headerOnly = append(headerOnly, 0x00, 0x10, 0x00, 0x10, 0x80, 0x00, 0x00)
	headerOnly = append(headerOnly, 0, 0, 0, 255, 255, 255)
	headerOnly = append(headerOnly, 0x2C, 0, 0, 0, 0, 0x00, 0x10, 0x00, 0x10, 0x00)
	headerOnly = append(headerOnly, 0x02, 0x01, 0x00, 0x00, 0x3B)
	for name, data := range map[string][]byte{"valid": valid, "header only": headerOnly} {
		t.Run(name, func(t *testing.T) {
			if len(data) > 64<<10 {
				t.Fatalf("fixture is %d bytes, want a small file", len(data))
			}
			got, err := decodeReplay(context.Background(), data, 384, 192)
			if !errors.Is(err, ErrReplayTooLong) {
				t.Fatalf("decodeReplay(context.Background(), ) error = %v, want ErrReplayTooLong before decoding", err)
			}
			if got.Poster != nil || got.Frames != nil {
				t.Fatal("an oversized replay still produced pixels")
			}
		})
	}
}

func TestDecodeReplayKeepsPosterWhenTooManyFrames(t *testing.T) {
	frames := make([]*image.Paletted, maxReplayFrames+50)
	for i := range frames {
		frames[i] = solidFrame(4, 4, uint8(i%2))
	}
	frames[len(frames)-1] = solidFrame(4, 4, 1)
	got, err := decodeReplay(context.Background(), encodeReplay(t, frames, 4, 4), 8, 8)
	if !errors.Is(err, ErrReplayTooLong) {
		t.Fatalf("decodeReplay(context.Background(), ) error = %v, want ErrReplayTooLong", err)
	}
	if got.Frames != nil {
		t.Fatalf("kept %d frames for a replay that is too long", len(got.Frames))
	}
	if got.Poster == nil {
		t.Fatal("too-long replay has no poster")
	}
	if r, g, b, _ := got.Poster.At(0, 0).RGBA(); r>>8 != 255 || g>>8 != 0 || b>>8 != 0 {
		t.Fatalf("poster = %d %d %d, want the last frame's red", r>>8, g>>8, b>>8)
	}
}

func TestDecodeReplayStopsKeepingFramesPastTheBudget(t *testing.T) {
	const w, h = 1200, 1000
	frames := make([]*image.Paletted, 10)
	for i := range frames {
		frames[i] = solidFrame(w, h, uint8(i%2))
	}
	got, err := decodeReplay(context.Background(), encodeReplay(t, frames, w, h), w, h)
	if !errors.Is(err, ErrReplayTooLong) {
		t.Fatalf("decodeReplay(context.Background(), ) error = %v, want ErrReplayTooLong", err)
	}
	if got.Frames != nil || got.Delays != nil {
		t.Fatalf("kept %d frames past the decoded budget", len(got.Frames))
	}
	if got.Poster == nil || got.Poster.Bounds().Dx() != w || got.Poster.Bounds().Dy() != h {
		t.Fatal("poster missing or wrongly sized")
	}
	if r, _, _, _ := got.Poster.At(0, 0).RGBA(); r>>8 != 255 {
		t.Fatal("poster is not the last frame")
	}
}

func transparentFrames(t *testing.T, n int) []byte {
	t.Helper()
	pal := color.Palette{color.RGBA{}, color.RGBA{255, 0, 0, 255}}
	for len(pal) < 256 {
		pal = append(pal, color.RGBA{0, 0, uint8(len(pal)), 255})
	}
	frame := image.NewPaletted(image.Rect(0, 0, 1, 1), pal)
	frame.Pix[0] = 1
	g := &gif.GIF{Config: image.Config{ColorModel: pal, Width: 1, Height: 1}}
	for range n {
		g.Image = append(g.Image, frame)
		g.Delay = append(g.Delay, 1)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodeReplayStopsAtFirstFramePastTheWorkBudget(t *testing.T) {
	// Twice the frames the budget admits, so stopping at the budget and
	// decoding the whole file are far apart.
	frames := 2 * (maxDecodeWork / (frameOverhead + 1))
	data := transparentFrames(t, frames)
	if len(data) > maxReplayBytes {
		t.Fatalf("fixture is %d bytes, over the file cap", len(data))
	}
	shape, err := scanGIF(data)
	if err != nil {
		t.Fatal(err)
	}
	if !shape.overWork || len(shape.frames) >= frames {
		t.Fatalf("scan read %d of %d frames, overWork = %v", len(shape.frames), frames, shape.overWork)
	}
	ctx := &cancelAfter{Context: context.Background(), after: math.MaxInt}
	got, err := decodeReplay(ctx, data, 8, 8)
	if !errors.Is(err, ErrReplayTooLong) {
		t.Fatalf("decodeReplay() error = %v, want ErrReplayTooLong", err)
	}
	if got.Poster == nil || got.Frames != nil {
		t.Fatalf("poster %v, %d frames; want a poster and no frames", got.Poster != nil, len(got.Frames))
	}
	if r, _, _, _ := got.Poster.At(0, 0).RGBA(); r>>8 != 255 {
		t.Fatal("poster lost the composited pixel")
	}
	// The context is checked once per composited frame. The last frame
	// scanned is the one that crossed the budget and is left out.
	if want := len(shape.frames) - 1; ctx.checks != want {
		t.Fatalf("composited %d frames, want the %d inside the work budget (file has %d)", ctx.checks, want, frames)
	}
}

func bandFrame(w, top, bottom int, pal color.Palette, index uint8) *image.Paletted {
	frame := image.NewPaletted(image.Rect(0, top, w, bottom), pal)
	for i := range frame.Pix {
		frame.Pix[i] = index
	}
	return frame
}

// recorderReplay is the shape fest writes: one solid full-canvas frame, then
// partial frames. Each partial frame paints the next band of rows.
func recorderReplay(t *testing.T, side, partial int) []byte {
	t.Helper()
	pal := color.Palette{color.RGBA{0, 0, 0, 255}, color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}}
	frames := []*image.Paletted{bandFrame(side, 0, side, pal, 1)}
	const band = 4
	for i := range partial {
		frames = append(frames, bandFrame(side, i*band, (i+1)*band, pal, 2))
	}
	return encodeReplay(t, frames, side, side)
}

func posterColours(poster image.Image) int {
	seen := map[color.RGBA]bool{}
	b := poster.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			seen[color.RGBAModel.Convert(poster.At(x, y)).(color.RGBA)] = true
		}
	}
	return len(seen)
}

func TestDecodeReplayOverWorkBudgetKeepsCompositedPoster(t *testing.T) {
	const side, partial = 1200, maxReplayFrames + 60
	data := recorderReplay(t, side, partial)
	if len(data) > maxReplayBytes {
		t.Fatalf("fixture is %d bytes, over the file cap", len(data))
	}
	shape, err := scanGIF(data)
	if err != nil {
		t.Fatal(err)
	}
	if !shape.overWork || len(shape.frames) < 3 {
		t.Fatalf("fixture did not trip the work budget: %d frames scanned, overWork = %v", len(shape.frames), shape.overWork)
	}
	got, err := decodeReplay(context.Background(), data, 120, 120)
	if !errors.Is(err, ErrReplayTooLong) {
		t.Fatalf("decodeReplay() error = %v, want ErrReplayTooLong", err)
	}
	if got.Frames != nil || got.Delays != nil {
		t.Fatalf("kept %d frames for a replay that is too long", len(got.Frames))
	}
	if got.Poster == nil {
		t.Fatal("over-budget replay has no poster")
	}
	if n := posterColours(got.Poster); n < 2 {
		t.Fatalf("poster has %d colour; the solid first frame was used instead of the composited canvas", n)
	}
}

// padPastSizeBudget grows a GIF beyond the file cap with a comment extension
// placed after its last frame, so every frame ends inside the cap.
func padPastSizeBudget(t *testing.T, data []byte) []byte {
	t.Helper()
	if data[len(data)-1] != 0x3B {
		t.Fatal("fixture does not end with a GIF trailer")
	}
	out := append([]byte{}, data[:len(data)-1]...)
	out = append(out, 0x21, 0xFE)
	block := append([]byte{255}, bytes.Repeat([]byte{'.'}, 255)...)
	for len(out) <= maxReplayBytes {
		out = append(out, block...)
	}
	return append(out, 0x00, 0x3B)
}

func TestDecodeReplayPastSizeBudgetKeepsPoster(t *testing.T) {
	data := padPastSizeBudget(t, recorderReplay(t, 64, 8))
	got, err := decodeReplay(context.Background(), data, 64, 64)
	if !errors.Is(err, ErrReplayTooLong) {
		t.Fatalf("decodeReplay() error = %v, want ErrReplayTooLong", err)
	}
	if got.Frames != nil || got.Delays != nil {
		t.Fatalf("kept %d frames for a file past the size budget", len(got.Frames))
	}
	if got.Poster == nil || posterColours(got.Poster) < 2 {
		t.Fatal("a file past the size budget lost the poster its first frames make")
	}

	small := recorderReplay(t, 64, 0)
	shape, err := scanGIF(small)
	if err != nil {
		t.Fatal(err)
	}
	// A first frame whose pixel data runs past the cap: nothing complete to draw.
	cut := append([]byte{}, small[:shape.header]...)
	cut = append(cut, 0x2C, 0, 0, 0, 0, 64, 0, 64, 0, 0x00, 0x02)
	block := append([]byte{255}, bytes.Repeat([]byte{0}, 255)...)
	for len(cut) <= maxReplayBytes {
		cut = append(cut, block...)
	}
	for name, data := range map[string][]byte{
		"not a GIF":       bytes.Repeat([]byte{'x'}, maxReplayBytes+1),
		"first frame cut": cut,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decodeReplay(context.Background(), data, 64, 64)
			if !errors.Is(err, ErrReplayTooLong) {
				t.Fatalf("decodeReplay() error = %v, want ErrReplayTooLong", err)
			}
			if got.Poster != nil || got.Frames != nil {
				t.Fatal("a file with no complete frame inside the size budget produced pixels")
			}
		})
	}
}

func TestScanGIFCountsPerFrameWork(t *testing.T) {
	global := color.Palette{color.RGBA{0, 0, 0, 255}, color.RGBA{255, 0, 0, 255}}
	local := color.Palette{color.RGBA{0, 0, 0, 255}, color.RGBA{0, 255, 0, 255}, color.RGBA{0, 0, 255, 255}, color.RGBA{255, 255, 255, 255}}
	g := &gif.GIF{
		Image: []*image.Paletted{
			image.NewPaletted(image.Rect(0, 0, 4, 4), global),
			image.NewPaletted(image.Rect(0, 0, 4, 4), local),
			image.NewPaletted(image.Rect(1, 1, 3, 3), global),
		},
		Delay:  []int{1, 1, 1},
		Config: image.Config{ColorModel: global, Width: 4, Height: 4},
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	shape, err := scanGIF(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.frames) != 3 || shape.overWork {
		t.Fatalf("frames = %d, overWork = %v", len(shape.frames), shape.overWork)
	}
	localCost := int64(len(local) * paletteEntryCost)
	if want := 5*(16+16+4) + 3*frameOverhead + localCost + 6*4*16; shape.work != want {
		t.Fatalf("work = %d, want %d (pixels, a per-frame term for each frame, and the local color table)", shape.work, want)
	}
	if want := 16 + localCost; shape.largest != want {
		t.Fatalf("largest frame cost = %d, want %d", shape.largest, want)
	}
	if want := 2*4*16 + shape.largest + frameOverhead; shape.memoryBytes() != want {
		t.Fatalf("memory = %d, want %d", shape.memoryBytes(), want)
	}
}

type cancelAfter struct {
	context.Context
	checks int
	after  int
}

func (c *cancelAfter) Err() error {
	c.checks++
	if c.checks >= c.after {
		return context.Canceled
	}
	return nil
}

func TestDecodeReplayStopsWhenCancelled(t *testing.T) {
	frames := make([]*image.Paletted, maxReplayFrames+100)
	for i := range frames {
		frames[i] = solidFrame(4, 4, uint8(i%2))
	}
	data := encodeReplay(t, frames, 4, 4)
	ctx := &cancelAfter{Context: context.Background(), after: 5}
	got, err := decodeReplay(ctx, data, 8, 8)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("decodeReplay() error = %v, want context.Canceled", err)
	}
	if got.Poster != nil || got.Frames != nil {
		t.Fatal("a cancelled decode still produced pixels")
	}
	if ctx.checks != ctx.after {
		t.Fatalf("decode checked the context %d times after it was cancelled at check %d; it kept decoding %d frames", ctx.checks, ctx.after, len(frames))
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DecodeReplay(cancelled, "/nonexistent/replay.gif", 8, 8); !errors.Is(err, context.Canceled) {
		t.Fatalf("DecodeReplay() with a cancelled context = %v, want context.Canceled before reading", err)
	}
}

func TestReplaySparseDisposalWorkIsBounded(t *testing.T) {
	pal := color.Palette{color.RGBA{}, color.RGBA{255, 0, 0, 255}}
	frame := image.NewPaletted(image.Rect(0, 0, 1, 1), pal)
	frame.Pix[0] = 1
	g := &gif.GIF{Config: image.Config{ColorModel: pal, Width: 3500, Height: 3500}}
	for range 30000 {
		g.Image = append(g.Image, frame)
		g.Delay = append(g.Delay, 1)
		g.Disposal = append(g.Disposal, gif.DisposalPrevious)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	if buf.Len() > maxReplayBytes {
		t.Fatal("fixture exceeds file budget")
	}
	shape, err := scanGIF(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !shape.overWork || len(shape.frames) >= maxReplayFrames {
		t.Fatalf("sparse canvas copying escaped budget: %+v", shape)
	}
	got, err := decodeReplay(context.Background(), buf.Bytes(), 8, 8)
	if !errors.Is(err, ErrReplayTooLong) || got.Poster == nil || len(got.Frames) != 0 {
		t.Fatalf("expected static poster: %v", err)
	}
	if r, _, _, _ := got.Poster.At(0, 0).RGBA(); r>>8 != 255 {
		t.Fatal("lost first-frame poster")
	}
}

func TestDecodeReplayThatEndsEarlyKeepsPoster(t *testing.T) {
	whole := recorderReplay(t, 64, 8)
	shape, err := scanGIF(whole)
	if err != nil {
		t.Fatal(err)
	}
	last := shape.frames[len(shape.frames)-1]
	for name, data := range map[string][]byte{
		"no trailer":         whole[:len(whole)-1],
		"cut in a frame":     whole[:last.start+(last.end-last.start)/2],
		"junk after a frame": append(append([]byte{}, whole[:last.start]...), 0x00, 0x01, 0x02),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decodeReplay(context.Background(), data, 64, 64)
			if !errors.Is(err, ErrReplayIncomplete) {
				t.Fatalf("decodeReplay() error = %v, want ErrReplayIncomplete with a poster", err)
			}
			if got.Frames != nil || got.Delays != nil {
				t.Fatalf("kept %d frames of a replay that ends early", len(got.Frames))
			}
			if got.Poster == nil || posterColours(got.Poster) < 2 {
				t.Fatal("a replay that ends early lost the poster its complete frames make")
			}
		})
	}
	// Nothing complete to draw is still a file that cannot be read.
	for name, data := range map[string][]byte{
		"cut in the first frame": whole[:shape.frames[0].start+(shape.frames[0].end-shape.frames[0].start)/2],
		"header only":            whole[:shape.header],
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decodeReplay(context.Background(), data, 64, 64)
			if err == nil || errors.Is(err, ErrReplayTooLong) || got.Poster != nil {
				t.Fatalf("decodeReplay() = poster %v, error %v; want a read failure", got.Poster != nil, err)
			}
		})
	}
}
