package explore

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const (
	maxReplayBytes  = 8 << 20
	maxReplayFrames = 200
	maxDecodedBytes = 32 << 20
	minFrameDelay   = 125 * time.Millisecond
)

// Frames is a decoded replay scaled to fit inside the stage.
type Frames struct {
	Poster image.Image
	Frames []image.Image
	Delays []time.Duration
}

// DecodeReplay composites a GIF and scales it to fit maxW by maxH.
// A file past the size or frame budget returns a poster and ErrReplayTooLong.
func DecodeReplay(path string, maxW, maxH int) (Frames, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Frames{}, camperrors.Wrap(err, "reading replay")
	}
	if !info.Mode().IsRegular() {
		return Frames{}, camperrors.New("replay is not a regular file")
	}
	if info.Size() > maxReplayBytes {
		return Frames{}, ErrReplayTooLong
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Frames{}, camperrors.Wrap(err, "reading replay")
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return Frames{}, camperrors.Wrap(err, "decoding replay")
	}
	if len(decoded.Image) == 0 {
		return Frames{}, camperrors.New("replay has no frames")
	}
	full := composite(decoded)
	if len(full) == 0 {
		return Frames{}, camperrors.New("replay has no frames")
	}
	poster := scale(full[len(full)-1], maxW, maxH)
	tooLong := len(full) > maxReplayFrames
	if !tooLong {
		b := poster.Bounds()
		pixels := b.Dx() * b.Dy() * 4 * len(full)
		tooLong = pixels > maxDecodedBytes
	}
	out := Frames{Poster: poster}
	if tooLong {
		return out, ErrReplayTooLong
	}
	out.Frames = make([]image.Image, len(full))
	out.Delays = make([]time.Duration, len(full))
	for i, frame := range full {
		out.Frames[i] = scale(frame, maxW, maxH)
		out.Delays[i] = frameDelay(decoded, i)
	}
	return out, nil
}

// EncodePNG encodes img as a PNG.
func EncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, camperrors.Wrap(err, "encoding poster")
	}
	return buf.Bytes(), nil
}

func frameDelay(g *gif.GIF, i int) time.Duration {
	if i >= len(g.Delay) {
		return minFrameDelay
	}
	d := time.Duration(g.Delay[i]) * 10 * time.Millisecond
	if d < minFrameDelay {
		return minFrameDelay
	}
	return d
}

func composite(g *gif.GIF) []image.Image {
	w, h := g.Config.Width, g.Config.Height
	if w <= 0 || h <= 0 {
		b := g.Image[0].Bounds()
		w, h = b.Dx(), b.Dy()
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	var saved *image.RGBA
	out := make([]image.Image, 0, len(g.Image))
	for i, frame := range g.Image {
		if i > 0 {
			switch disposal(g, i-1) {
			case gif.DisposalBackground:
				clearRect(canvas, g.Image[i-1].Bounds())
			case gif.DisposalPrevious:
				if saved != nil {
					copyRGBA(canvas, saved)
				}
			}
		}
		if disposal(g, i) == gif.DisposalPrevious {
			saved = cloneRGBA(canvas)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		out = append(out, cloneRGBA(canvas))
	}
	return out
}

func disposal(g *gif.GIF, i int) byte {
	if i < 0 || i >= len(g.Disposal) {
		return gif.DisposalNone
	}
	return g.Disposal[i]
}

func clearRect(dst *image.RGBA, rect image.Rectangle) {
	rect = rect.Intersect(dst.Bounds())
	clear := image.NewUniform(color.RGBA{})
	draw.Draw(dst, rect, clear, image.Point{}, draw.Src)
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	copyRGBA(dst, src)
	return dst
}

func copyRGBA(dst, src *image.RGBA) {
	copy(dst.Pix, src.Pix)
}

func scale(src image.Image, maxW, maxH int) image.Image {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 || maxW <= 0 || maxH <= 0 {
		return src
	}
	dw, dh := sw, sh
	if dw > maxW {
		dh = dh * maxW / dw
		dw = maxW
	}
	if dh > maxH {
		dw = dw * maxH / dh
		dh = maxH
	}
	if dw <= 0 {
		dw = 1
	}
	if dh <= 0 {
		dh = 1
	}
	if dw == sw && dh == sh {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		sy := b.Min.Y + y*sh/dh
		for x := range dw {
			sx := b.Min.X + x*sw/dw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}
