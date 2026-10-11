package explore

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
)

func TestReplayBackgroundDisposal(t *testing.T) {
	blue := color.RGBA{0, 0, 255, 255}
	green := color.RGBA{0, 180, 0, 255}
	red := color.RGBA{255, 0, 0, 255}
	yellow := color.RGBA{255, 255, 0, 255}
	global := color.Palette{color.Black, blue, green, color.White}
	// Index 2 deliberately differs from the global background; index 0 is
	// transparent so the third frame exposes the disposed rectangle beneath it.
	local := color.Palette{color.RGBA{}, red, color.RGBA{255, 0, 255, 255}, yellow}
	for _, tc := range []struct {
		name         string
		global       bool
		invalidIndex bool
		posterOnly   bool
		background   color.RGBA
	}{
		{name: "global background", global: true, background: green},
		{name: "poster only", global: true, posterOnly: true, background: green},
		{name: "no global palette"},
		{name: "invalid background index", global: true, invalidIndex: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := image.NewPaletted(image.Rect(0, 0, 4, 3), global)
			for i := range first.Pix {
				first.Pix[i] = 1
			}
			second := image.NewPaletted(image.Rect(1, 0, 3, 2), local)
			for i := range second.Pix {
				second.Pix[i] = 1
			}
			third := image.NewPaletted(image.Rect(0, 0, 4, 3), local)
			third.SetColorIndex(3, 2, 3)
			g := &gif.GIF{Image: []*image.Paletted{first, second, third}, Delay: []int{1, 1, 1}, Disposal: []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalNone}, Config: image.Config{Width: 4, Height: 3}, BackgroundIndex: 2}
			if tc.global {
				g.Config.ColorModel = global
			}
			if tc.posterOnly {
				for len(g.Image) <= maxReplayFrames {
					g.Image = append(g.Image, third)
					g.Delay = append(g.Delay, 1)
					g.Disposal = append(g.Disposal, gif.DisposalNone)
				}
			}
			var data bytes.Buffer
			if err := gif.EncodeAll(&data, g); err != nil {
				t.Fatal(err)
			}
			encoded := data.Bytes()
			if tc.invalidIndex {
				encoded[11] = 255
			}
			got, err := decodeReplay(context.Background(), encoded, 4, 3)
			if tc.posterOnly {
				if !errors.Is(err, ErrReplayTooLong) || len(got.Frames) != 0 {
					t.Fatalf("poster fallback: %v, %d frames", err, len(got.Frames))
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if len(got.Frames) != 3 {
					t.Fatalf("got %d frames", len(got.Frames))
				}
				assertReplayPixel(t, got.Frames[0], 1, 0, blue)
				assertReplayPixel(t, got.Frames[1], 1, 0, red)
				assertReplayPixel(t, got.Frames[2], 1, 0, tc.background)
				assertReplayPixel(t, got.Frames[2], 0, 0, blue)
				assertReplayPixel(t, got.Frames[2], 3, 2, yellow)
			}
			// Verify the actual PNG representation sent to terminal image protocols,
			// including alpha, rather than just the intermediate RGBA canvas.
			encodedPoster, err := EncodePNG(got.Poster)
			if err != nil {
				t.Fatal(err)
			}
			poster, err := png.Decode(bytes.NewReader(encodedPoster))
			if err != nil {
				t.Fatal(err)
			}
			for y := range 3 {
				for x := range 4 {
					want := blue
					if image.Pt(x, y).In(second.Bounds()) {
						want = tc.background
					}
					if x == 3 && y == 2 {
						want = yellow
					}
					assertReplayPixel(t, poster, x, y, want)
				}
			}
		})
	}
}

func TestReplayInitialBackgroundAndPreviousDisposal(t *testing.T) {
	green := color.RGBA{0, 180, 0, 255}
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	pal := color.Palette{red, blue, green, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 1, 1), pal)
	second := image.NewPaletted(image.Rect(1, 0, 2, 1), pal)
	second.Pix[0] = 1
	third := image.NewPaletted(image.Rect(2, 0, 3, 1), pal)
	g := &gif.GIF{Image: []*image.Paletted{first, second, third}, Delay: []int{1, 1, 1}, Disposal: []byte{gif.DisposalNone, gif.DisposalPrevious, gif.DisposalNone}, Config: image.Config{ColorModel: pal, Width: 3, Height: 1}, BackgroundIndex: 2}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	got, err := decodeReplay(context.Background(), buf.Bytes(), 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertReplayPixel(t, got.Frames[0], 1, 0, green)
	assertReplayPixel(t, got.Frames[1], 1, 0, blue)
	assertReplayPixel(t, got.Frames[2], 1, 0, green)
	assertReplayPixel(t, got.Poster, 0, 0, red)
	assertReplayPixel(t, got.Poster, 2, 0, red)
}

func assertReplayPixel(t *testing.T, img image.Image, x, y int, want color.RGBA) {
	t.Helper()
	got := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
	if got != want {
		t.Errorf("pixel (%d,%d) = %#v; want %#v", x, y, got, want)
	}
}
