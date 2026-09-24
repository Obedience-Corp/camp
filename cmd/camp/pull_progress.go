package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	pullsvc "github.com/Obedience-Corp/camp/internal/pull"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

const (
	pullProgressTick     = 100 * time.Millisecond
	pullProgressBarWidth = 24
	eraseLineAbove       = "\x1b[1A\x1b[2K"
)

var pullSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// pullProgressLive reports whether camp pull all should redraw a live progress
// area. Pipes, files, and dumb terminals get the plain line-per-repo output.
func pullProgressLive(f *os.File) bool {
	return term.IsTerminal(int(f.Fd())) && os.Getenv("TERM") != "dumb"
}

type pullActiveRow struct {
	target  pullsvc.Target
	order   int
	started time.Time
}

// pullProgress keeps finished repos as permanent lines, in declaration order,
// above a redrawn area listing the repos pulling right now and a progress bar.
// The redrawn area is at most one row per worker plus the bar, so it stays on
// screen and can always be erased by moving the cursor up over it.
type pullProgress struct {
	mu       sync.Mutex
	out      io.Writer
	fd       int
	styles   pullStyles
	now      func() time.Time
	started  time.Time
	total    int
	order    map[string]int
	finished int
	active   []pullActiveRow
	drawn    int
	frame    int
	running  bool
	closed   bool
	stop     chan struct{}
	ticker   sync.WaitGroup
}

func newPullProgress(out io.Writer, fd int, styles pullStyles) *pullProgress {
	return &pullProgress{
		out:    out,
		fd:     fd,
		styles: styles,
		now:    time.Now,
		stop:   make(chan struct{}),
	}
}

// Start records the repos in declaration order and begins animating the spinner.
func (p *pullProgress) Start(targets []pullsvc.Target) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.running {
		return
	}
	p.total = len(targets)
	p.order = make(map[string]int, len(targets))
	for i, t := range targets {
		p.order[t.Path] = i
	}
	p.started = p.now()
	p.running = true
	p.redraw("")

	p.ticker.Go(func() {
		t := time.NewTicker(pullProgressTick)
		defer t.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-t.C:
				p.mu.Lock()
				p.frame++
				p.redraw("")
				p.mu.Unlock()
			}
		}
	})
}

// Begin adds a repo to the live area. Safe for concurrent use.
func (p *pullProgress) Begin(t pullsvc.Target) {
	p.mu.Lock()
	defer p.mu.Unlock()
	row := pullActiveRow{target: t, order: p.order[t.Path], started: p.now()}
	i, _ := slices.BinarySearchFunc(p.active, row.order, func(r pullActiveRow, order int) int {
		return r.order - order
	})
	p.active = slices.Insert(p.active, i, row)
	p.redraw("")
}

// Finish removes a repo from the live area and advances the bar. Safe for
// concurrent use.
func (p *pullProgress) Finish(r pullsvc.Result) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, row := range p.active {
		if row.target.Path == r.Target.Path {
			p.active = append(p.active[:i], p.active[i+1:]...)
			break
		}
	}
	p.finished++
	p.redraw("")
}

// Commit prints a permanent line above the live area.
func (p *pullProgress) Commit(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_, _ = fmt.Fprintln(p.out, line)
		return
	}
	p.redraw(line + "\n")
}

// Close erases the live area and stops the spinner. It is idempotent, so every
// exit path can call it.
func (p *pullProgress) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	_, _ = fmt.Fprint(p.out, p.erase())
	running := p.running
	p.mu.Unlock()

	if running {
		close(p.stop)
		p.ticker.Wait()
	}
}

// redraw writes one frame: erase the old area, print any committed text, then
// draw the new area. It is a single write so the terminal never shows a
// half-drawn frame. Callers hold p.mu.
func (p *pullProgress) redraw(committed string) {
	if p.closed {
		return
	}
	var b strings.Builder
	b.WriteString(p.erase())
	b.WriteString(committed)
	if p.running {
		lines := p.area()
		for _, l := range lines {
			b.WriteString(l)
			b.WriteByte('\n')
		}
		p.drawn = len(lines)
	}
	_, _ = fmt.Fprint(p.out, b.String())
}

func (p *pullProgress) erase() string {
	s := strings.Repeat(eraseLineAbove, p.drawn) + "\r"
	p.drawn = 0
	return s
}

func (p *pullProgress) area() []string {
	width, height := 80, 24
	if w, h, err := term.GetSize(p.fd); err == nil && w > 0 && h > 0 {
		width, height = w, h
	}
	fit := lipgloss.NewStyle().MaxWidth(width - 1)

	rows := p.active
	maxRows := max(height-3, 0)
	var more int
	if len(rows) > maxRows {
		more = len(rows) - max(maxRows-1, 0)
		rows = rows[:max(maxRows-1, 0)]
	}

	now := p.now()
	spinner := p.styles.yellow.Render(pullSpinnerFrames[p.frame%len(pullSpinnerFrames)])
	lines := make([]string, 0, len(rows)+2)
	for _, row := range rows {
		lines = append(lines, fit.Render(fmt.Sprintf(" %s %-30s %s  %s",
			spinner, row.target.Name,
			p.styles.dim.Render(row.target.Branch),
			p.styles.dim.Render(formatPullElapsed(now.Sub(row.started))))))
	}
	if more > 0 {
		lines = append(lines, fit.Render(p.styles.dim.Render(fmt.Sprintf("   … %d more pulling", more))))
	}
	lines = append(lines, fit.Render(fmt.Sprintf("   %s %d/%d  %s",
		p.bar(), p.finished, p.total,
		p.styles.dim.Render(formatPullElapsed(now.Sub(p.started))))))
	return lines
}

func (p *pullProgress) bar() string {
	filled := 0
	if p.total > 0 {
		filled = p.finished * pullProgressBarWidth / p.total
	}
	return p.styles.green.Render(strings.Repeat("━", filled)) +
		p.styles.dim.Render(strings.Repeat("━", pullProgressBarWidth-filled))
}

func formatPullElapsed(d time.Duration) string {
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// newLivePullHooks renders camp pull all through a pullProgress. The returned
// close function erases the live area; call it on every exit path.
func newLivePullHooks(out *os.File) (pullsvc.Hooks, func()) {
	styles := newPullStyles()
	p := newPullProgress(out, int(out.Fd()), styles)

	var originalBranch string
	hooks := pullsvc.Hooks{
		OnStart:   renderPullStart,
		OnTargets: p.Start,
		OnBegin:   p.Begin,
		OnFinish:  p.Finish,
		OnSkip: func(t pullsvc.Target, status string) {
			p.Commit(fmt.Sprintf(" %s %-30s %s", styles.yellow.Render("–"), t.Name, styles.yellow.Render(status)))
		},
		OnPulling: func(_ pullsvc.Target, original string) { originalBranch = original },
		OnResult: func(r pullsvc.Result) {
			p.Commit(formatPullLiveResult(r, originalBranch, styles))
		},
		OnChangedRefs: func(paths []string) {
			p.Close()
			renderChangedRefs(paths, styles)
		},
		OnSummary: func(s pullsvc.Summary) {
			p.Close()
			renderPullSummary(s, styles)
		},
	}
	return hooks, p.Close
}

func formatPullSkip(t pullsvc.Target, status string, styles pullStyles) string {
	return fmt.Sprintf("  %-30s %s", t.Name, styles.yellow.Render(status))
}

func formatPullLiveResult(r pullsvc.Result, originalBranch string, styles pullStyles) string {
	branch := styles.dim.Render(r.Target.Branch)
	if originalBranch != "" && originalBranch != "HEAD" && originalBranch != r.Target.Branch {
		branch += "  " + styles.dim.Render(fmt.Sprintf("(was %s)", originalBranch))
	}
	return fmt.Sprintf(" %s %-30s %s  %s", pullResultMark(r, styles), r.Target.Name, branch, pullResultStatus(r, styles))
}

func pullResultMark(r pullsvc.Result, styles pullStyles) string {
	switch r.Outcome {
	case pullsvc.OutcomePulled:
		return styles.green.Render("✓")
	case pullsvc.OutcomeFailed:
		return styles.red.Render("✗")
	default:
		return styles.dim.Render("·")
	}
}

func pullResultStatus(r pullsvc.Result, styles pullStyles) string {
	switch r.Outcome {
	case pullsvc.OutcomePulled:
		return styles.green.Render(r.Status)
	case pullsvc.OutcomeFailed:
		return styles.red.Render(r.Status)
	default:
		return styles.dim.Render(r.Status)
	}
}
