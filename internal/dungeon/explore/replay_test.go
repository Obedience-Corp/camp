package explore

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
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
	got, err := decodeReplay(buf.Bytes(), 8, 8)
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
	seq := KittyPNG(7, png)
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
			got, err := decodeReplay(data, 384, 192)
			if !errors.Is(err, ErrReplayTooLong) {
				t.Fatalf("decodeReplay() error = %v, want ErrReplayTooLong before decoding", err)
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
	got, err := decodeReplay(encodeReplay(t, frames, 4, 4), 8, 8)
	if !errors.Is(err, ErrReplayTooLong) {
		t.Fatalf("decodeReplay() error = %v, want ErrReplayTooLong", err)
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
	got, err := decodeReplay(encodeReplay(t, frames, w, h), w, h)
	if !errors.Is(err, ErrReplayTooLong) {
		t.Fatalf("decodeReplay() error = %v, want ErrReplayTooLong", err)
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
