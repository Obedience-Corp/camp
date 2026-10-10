package explore

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
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
	path := filepath.Join(t.TempDir(), "replay.gif")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeReplay(path, 8, 8)
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
	if del != "\033_Ga=d,d=i,i=7\033\\" {
		t.Fatalf("delete = %q", del)
	}
}
