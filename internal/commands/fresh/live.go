package fresh

import (
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Obedience-Corp/obey-shared/brand"
	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/ui"
)

const freshSpinnerTick = 80 * time.Millisecond

var freshSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// freshMotion reports whether a step may redraw its own row in place.
// Pipes, dumb terminals, and reduced-motion sessions get one settled row.
func freshMotion(f *os.File) bool {
	if f == nil || brand.ReducedMotion() || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// freshLive is the row for a step that takes long enough to watch.
// On a terminal it redraws one line with a spinner. The first byte of child
// output commits that line so the stream is not erased. A pipe prints only
// the settled row.
type freshLive struct {
	out         io.Writer
	width       int
	label       string
	status      string
	detail      string
	animated    bool
	mu          sync.Mutex
	frame       int
	drawn       int
	frozen      bool
	detailShown bool
	stop        chan struct{}
	stopOnce    sync.Once
	ticks       sync.WaitGroup
}

func newFreshLive(out io.Writer, label, status, detail string) *freshLive {
	return &freshLive{
		out:      out,
		width:    ui.TermColumns(),
		label:    label,
		status:   status,
		detail:   detail,
		animated: freshMotion(os.Stdout),
		stop:     make(chan struct{}),
	}
}

// Begin shows the working row. A pipe writes the command detail, when there
// is one, and leaves the result row to Finish.
func (s *freshLive) Begin() {
	if !s.animated {
		s.writeDetail()
		return
	}
	s.mu.Lock()
	s.paintLocked()
	s.mu.Unlock()
	s.ticks.Go(func() {
		ticker := time.NewTicker(freshSpinnerTick)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				s.mu.Lock()
				if s.frozen {
					s.mu.Unlock()
					return
				}
				s.frame++
				s.paintLocked()
				s.mu.Unlock()
			}
		}
	})
}

// Stream writes child output. The first byte stops the spinner and commits
// the working row so later bytes stay on screen.
func (s *freshLive) Stream() io.Writer {
	return &freshLiveStream{live: s, dst: s.out}
}

// Finish replaces the spinner with the settled row. Child output clears the
// spinner first, and the result is printed under that output.
func (s *freshLive) Finish(status string, tone ui.StatusTone) {
	if s.animated {
		s.stopAnimation()
	}
	s.mu.Lock()
	frozen := s.frozen
	if s.animated && !frozen {
		s.eraseLocked()
	}
	s.mu.Unlock()
	_, _ = io.WriteString(s.out, s.renderRow(ui.ChecklistMark(tone), status, tone))
	if !frozen {
		s.writeDetail()
	}
}

// Freeze removes the spinner and prints the command line. Child output then
// follows, and Finish adds the single result row under it.
func (s *freshLive) Freeze() {
	s.mu.Lock()
	if s.frozen {
		s.mu.Unlock()
		return
	}
	s.frozen = true
	animated := s.animated
	s.mu.Unlock()
	if !animated {
		return
	}
	s.stopAnimation()
	s.mu.Lock()
	s.eraseLocked()
	s.mu.Unlock()
	s.writeDetail()
}

func (s *freshLive) stopAnimation() {
	s.stopOnce.Do(func() {
		close(s.stop)
		s.ticks.Wait()
	})
}

func (s *freshLive) paintLocked() {
	if s.drawn > 0 {
		s.eraseLocked()
	}
	mark := ui.Accent(freshSpinnerFrames[s.frame%len(freshSpinnerFrames)]) + " "
	_, _ = io.WriteString(s.out, s.renderRow(mark, s.status, ui.StatusMuted))
	s.drawn = 1
}

func (s *freshLive) eraseLocked() {
	if s.drawn == 0 {
		return
	}
	_, _ = io.WriteString(s.out, strings.Repeat("\x1b[1A\x1b[2K", s.drawn))
	s.drawn = 0
}

func (s *freshLive) renderRow(mark, status string, tone ui.StatusTone) string {
	var b strings.Builder
	_ = ui.WriteChecklistRow(&b, s.width, ui.ChecklistRowIndent, mark, s.label, status, tone)
	return b.String()
}

func (s *freshLive) writeDetail() {
	if s.detail == "" || s.detailShown {
		return
	}
	s.detailShown = true
	writeFreshDetail(s.out, s.width, s.detail)
}

type freshLiveStream struct {
	live *freshLive
	once sync.Once
	dst  io.Writer
}

func (w *freshLiveStream) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.once.Do(w.live.Freeze)
	}
	return w.dst.Write(p)
}
