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
	// maxDecodeBytes bounds what gif.DecodeAll and compositing hold at once:
	// one palette byte per pixel of every frame plus two RGBA canvases.
	maxDecodeBytes = 96 << 20
	minFrameDelay  = 125 * time.Millisecond
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
	return decodeReplay(data, maxW, maxH)
}

func decodeReplay(data []byte, maxW, maxH int) (Frames, error) {
	shape, err := scanGIF(data)
	if err != nil {
		return Frames{}, camperrors.Wrap(err, "decoding replay")
	}
	if shape.frames == 0 {
		return Frames{}, camperrors.New("replay has no frames")
	}
	if shape.decodeBytes() > maxDecodeBytes {
		return Frames{}, ErrReplayTooLong
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return Frames{}, camperrors.Wrap(err, "decoding replay")
	}
	if len(decoded.Image) == 0 {
		return Frames{}, camperrors.New("replay has no frames")
	}
	out, tooLong := composite(decoded, maxW, maxH)
	if tooLong {
		return out, ErrReplayTooLong
	}
	return out, nil
}

// gifShape is what a GIF will cost to decode, read from its block structure
// without decompressing any image data.
type gifShape struct {
	width  int
	height int
	frames int
	pixels int
}

func (s gifShape) decodeBytes() int {
	return s.pixels + 2*4*s.width*s.height
}

// scanGIF walks the GIF block structure: header, color tables, extensions,
// and image descriptors. It stops counting once the decode budget is spent.
func scanGIF(data []byte) (gifShape, error) {
	if len(data) < 13 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
		return gifShape{}, errNotGIF
	}
	shape := gifShape{
		width:  int(data[6]) | int(data[7])<<8,
		height: int(data[8]) | int(data[9])<<8,
	}
	pos := 13 + colorTableLen(data[10])
	for pos < len(data) {
		switch data[pos] {
		case 0x21:
			next, err := skipSubBlocks(data, pos+2)
			if err != nil {
				return gifShape{}, err
			}
			pos = next
		case 0x2C:
			if pos+10 > len(data) {
				return gifShape{}, errNotGIF
			}
			w := int(data[pos+5]) | int(data[pos+6])<<8
			h := int(data[pos+7]) | int(data[pos+8])<<8
			shape.frames++
			shape.pixels += w * h
			if shape.decodeBytes() > maxDecodeBytes {
				return shape, nil
			}
			next, err := skipSubBlocks(data, pos+10+colorTableLen(data[pos+9])+1)
			if err != nil {
				return gifShape{}, err
			}
			pos = next
		case 0x3B:
			return shape, nil
		default:
			return gifShape{}, errNotGIF
		}
	}
	return gifShape{}, errNotGIF
}

func colorTableLen(fields byte) int {
	if fields&0x80 == 0 {
		return 0
	}
	return 3 << (1 + uint(fields&0x07))
}

func skipSubBlocks(data []byte, pos int) (int, error) {
	for pos < len(data) {
		n := int(data[pos])
		pos++
		if n == 0 {
			return pos, nil
		}
		pos += n
	}
	return 0, errNotGIF
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

// composite draws every frame onto one canvas and keeps a scaled copy of each
// until the frame or decoded budget runs out. Past that it keeps drawing so
// the poster still shows the final frame, and reports the replay too long.
func composite(g *gif.GIF, maxW, maxH int) (Frames, bool) {
	w, h := g.Config.Width, g.Config.Height
	if w <= 0 || h <= 0 {
		b := g.Image[0].Bounds()
		w, h = b.Dx(), b.Dy()
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	var saved *image.RGBA
	var out Frames
	tooLong := len(g.Image) > maxReplayFrames
	kept := 0
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
		if tooLong {
			continue
		}
		shot := snapshot(canvas, maxW, maxH)
		b := shot.Bounds()
		kept += b.Dx() * b.Dy() * 4
		if kept > maxDecodedBytes {
			tooLong = true
			out.Frames, out.Delays = nil, nil
			continue
		}
		out.Frames = append(out.Frames, shot)
		out.Delays = append(out.Delays, frameDelay(g, i))
	}
	out.Poster = snapshot(canvas, maxW, maxH)
	if tooLong {
		out.Frames, out.Delays = nil, nil
	}
	return out, tooLong
}

// snapshot scales canvas to fit, copying it when no scaling was needed so
// later frames do not draw over the result.
func snapshot(canvas *image.RGBA, maxW, maxH int) image.Image {
	img := scale(canvas, maxW, maxH)
	if same, ok := img.(*image.RGBA); ok && same == canvas {
		return cloneRGBA(canvas)
	}
	return img
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
