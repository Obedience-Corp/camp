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
	maxReplayBytes   = 8 << 20
	maxReplayFrames  = 200
	maxDecodedBytes  = 32 << 20
	maxDecodeBytes   = 96 << 20
	maxDecodeWork    = 1 << 30
	frameOverhead    = 32 << 10
	paletteEntryCost = 20
	minFrameDelay    = 125 * time.Millisecond
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
	if len(shape.frames) == 0 || shape.width <= 0 || shape.height <= 0 {
		return Frames{}, camperrors.New("replay has no frames")
	}
	if shape.memoryBytes() > maxDecodeBytes {
		return Frames{}, ErrReplayTooLong
	}
	spans := shape.frames
	tooLong := shape.overWork || len(spans) > maxReplayFrames
	if shape.overWork {
		spans = spans[:1]
	}
	out, tooLong, err := composite(data, shape, spans, tooLong, maxW, maxH)
	if err != nil {
		return Frames{}, camperrors.Wrap(err, "decoding replay")
	}
	if tooLong {
		return out, ErrReplayTooLong
	}
	return out, nil
}

type gifSpan struct {
	start int
	end   int
}

type gifShape struct {
	width    int
	height   int
	header   int
	frames   []gifSpan
	widest   int
	largest  int
	work     int
	overWork bool
}

func (s gifShape) memoryBytes() int {
	return 2*4*s.width*s.height + s.largest + frameOverhead
}

func scanGIF(data []byte) (gifShape, error) {
	if len(data) < 13 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
		return gifShape{}, errNotGIF
	}
	shape := gifShape{
		width:  int(data[6]) | int(data[7])<<8,
		height: int(data[8]) | int(data[9])<<8,
		header: 13 + colorTableLen(data[10]),
	}
	pos, start := shape.header, shape.header
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
			table := colorTableLen(data[pos+9])
			next, err := skipSubBlocks(data, pos+10+table+1)
			if err != nil {
				return gifShape{}, err
			}
			cost := w*h + table/3*paletteEntryCost
			shape.frames = append(shape.frames, gifSpan{start: start, end: next})
			shape.widest = max(shape.widest, next-start)
			shape.largest = max(shape.largest, cost)
			shape.work += cost + frameOverhead
			pos, start = next, next
			if shape.work > maxDecodeWork {
				shape.overWork = true
				return shape, nil
			}
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

func composite(data []byte, shape gifShape, spans []gifSpan, tooLong bool, maxW, maxH int) (Frames, bool, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, shape.width, shape.height))
	var saved *image.RGBA
	var prev image.Rectangle
	prevDisposal := byte(gif.DisposalNone)
	buf := make([]byte, 0, shape.header+shape.widest+1)
	var out Frames
	kept := 0
	for i, span := range spans {
		frame, delay, disposal, err := decodeFrame(&buf, data, shape.header, span)
		if err != nil {
			return Frames{}, false, err
		}
		if i > 0 {
			switch prevDisposal {
			case gif.DisposalBackground:
				clearRect(canvas, prev)
			case gif.DisposalPrevious:
				if saved != nil {
					copyRGBA(canvas, saved)
				}
			}
		}
		if disposal == gif.DisposalPrevious {
			if saved == nil {
				saved = cloneRGBA(canvas)
			} else {
				copyRGBA(saved, canvas)
			}
		}
		drawFrame(canvas, frame)
		prev, prevDisposal = frame.Bounds(), disposal
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
		out.Delays = append(out.Delays, delay)
	}
	out.Poster = snapshot(canvas, maxW, maxH)
	if tooLong {
		out.Frames, out.Delays = nil, nil
	}
	return out, tooLong, nil
}

func drawFrame(dst *image.RGBA, src *image.Paletted) {
	var lut [256][4]uint8
	var opaque [256]bool
	for i, c := range src.Palette {
		r, g, b, a := c.RGBA()
		if a == 0 {
			continue
		}
		if a != 0xffff {
			draw.Draw(dst, src.Bounds(), src, src.Bounds().Min, draw.Over)
			return
		}
		lut[i] = [4]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 0xff}
		opaque[i] = true
	}
	area := src.Bounds().Intersect(dst.Bounds())
	for y := area.Min.Y; y < area.Max.Y; y++ {
		si := src.PixOffset(area.Min.X, y)
		di := dst.PixOffset(area.Min.X, y)
		for range area.Dx() {
			if idx := src.Pix[si]; opaque[idx] {
				copy(dst.Pix[di:di+4], lut[idx][:])
			}
			si++
			di += 4
		}
	}
}

func decodeFrame(buf *[]byte, data []byte, header int, span gifSpan) (*image.Paletted, time.Duration, byte, error) {
	one := append((*buf)[:0], data[:header]...)
	one = append(one, data[span.start:span.end]...)
	one = append(one, 0x3B)
	*buf = one
	g, err := gif.DecodeAll(bytes.NewReader(one))
	if err != nil {
		return nil, 0, 0, err
	}
	if len(g.Image) != 1 {
		return nil, 0, 0, errNotGIF
	}
	disposal := byte(gif.DisposalNone)
	if len(g.Disposal) > 0 {
		disposal = g.Disposal[0]
	}
	return g.Image[0], frameDelay(g, 0), disposal, nil
}

func snapshot(canvas *image.RGBA, maxW, maxH int) image.Image {
	img := scale(canvas, maxW, maxH)
	if same, ok := img.(*image.RGBA); ok && same == canvas {
		return cloneRGBA(canvas)
	}
	return img
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
