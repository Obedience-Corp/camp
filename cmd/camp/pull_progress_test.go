package main

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	pullsvc "github.com/Obedience-Corp/camp/internal/pull"
	"github.com/charmbracelet/x/ansi"
)

// screenLines applies the only cursor controls pullProgress emits (erase the
// line above, carriage return, newline) and returns the visible lines, so the
// assertions read what a terminal would show rather than raw bytes.
func screenLines(t *testing.T, raw string) []string {
	t.Helper()
	var lines []string
	var cur strings.Builder
	for len(raw) > 0 {
		switch {
		case strings.HasPrefix(raw, eraseLineAbove):
			if len(lines) == 0 {
				t.Fatalf("erased above the first line: the live area over-erased")
			}
			lines = lines[:len(lines)-1]
			raw = raw[len(eraseLineAbove):]
		case raw[0] == '\r':
			if cur.Len() != 0 {
				t.Fatalf("carriage return over unterminated text %q", cur.String())
			}
			raw = raw[1:]
		case raw[0] == '\n':
			lines = append(lines, ansi.Strip(cur.String()))
			cur.Reset()
			raw = raw[1:]
		default:
			cur.WriteByte(raw[0])
			raw = raw[1:]
		}
	}
	if cur.Len() != 0 {
		lines = append(lines, ansi.Strip(cur.String()))
	}
	return lines
}

func newTestPullProgress(out *bytes.Buffer) *pullProgress {
	p := newPullProgress(out, -1, newPullStyles())
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return base }
	return p
}

func target(name string) pullsvc.Target {
	return pullsvc.Target{Name: name, Path: "/c/projects/" + name, Branch: "main"}
}

func targets(n int) []pullsvc.Target {
	ts := make([]pullsvc.Target, n)
	for i := range ts {
		ts[i] = target(fmt.Sprintf("repo-%02d", i))
	}
	return ts
}

func TestPullProgressListsPullingReposInDeclarationOrder(t *testing.T) {
	var out bytes.Buffer
	p := newTestPullProgress(&out)
	p.Start(targets(5))
	for _, i := range []int{3, 0, 4, 1} {
		p.Begin(target(fmt.Sprintf("repo-%02d", i)))
	}

	var names []string
	for _, row := range p.active {
		names = append(names, row.target.Name)
	}
	want := []string{"repo-00", "repo-01", "repo-03", "repo-04"}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("pulling rows = %v, want declaration order %v", names, want)
	}
}

func TestPullProgressLeavesOnlyCommittedLinesAfterClose(t *testing.T) {
	var out bytes.Buffer
	p := newTestPullProgress(&out)

	p.Start([]pullsvc.Target{target("alpha"), target("bravo"), target("charlie")})
	p.Begin(target("alpha"))
	p.Begin(target("bravo"))
	p.Begin(target("charlie"))
	p.Finish(pullsvc.Result{Target: target("bravo")})
	p.Commit("  alpha done")

	mid := screenLines(t, out.String())
	if mid[0] != "  alpha done" {
		t.Fatalf("committed line should sit above the live area, screen = %q", mid)
	}
	live := strings.Join(mid[1:], "\n")
	for _, want := range []string{"alpha", "charlie", "1/3"} {
		if !strings.Contains(live, want) {
			t.Fatalf("live area missing %q:\n%s", want, live)
		}
	}
	if strings.Contains(live, "bravo") {
		t.Fatalf("finished repo still shown as pulling:\n%s", live)
	}

	p.Commit("  bravo up-to-date")
	p.Close()
	p.Close()

	got := screenLines(t, out.String())
	want := []string{"  alpha done", "  bravo up-to-date"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("screen after close = %q, want %q", got, want)
	}
}

func TestPullProgressCommitAfterClosePrintsPlainLine(t *testing.T) {
	var out bytes.Buffer
	p := newTestPullProgress(&out)
	p.Start([]pullsvc.Target{target("alpha")})
	p.Close()
	p.Commit("  late line")

	got := screenLines(t, out.String())
	if len(got) != 1 || got[0] != "  late line" {
		t.Fatalf("screen = %q, want only the late line", got)
	}
}

func TestPullProgressCapsLiveAreaToTerminalHeight(t *testing.T) {
	var out bytes.Buffer
	p := newTestPullProgress(&out)
	p.Start(targets(40))
	for i := range 30 {
		p.Begin(target(fmt.Sprintf("repo-%02d", i)))
	}

	lines := p.area()
	// fd -1 has no size, so the renderer assumes 80x24.
	if len(lines) > 24-2 {
		t.Fatalf("live area is %d lines, must fit a 24-line terminal with room to spare", len(lines))
	}
	joined := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "more pulling") {
		t.Fatalf("overflow should be summarized:\n%s", joined)
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w >= 80 {
			t.Fatalf("line is %d cells wide, would wrap an 80-column terminal: %q", w, ansi.Strip(l))
		}
	}
}

func TestPullProgressConcurrentWorkersKeepScreenConsistent(t *testing.T) {
	var out bytes.Buffer
	p := newTestPullProgress(&out)
	p.Start(targets(50))

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			tg := target(fmt.Sprintf("repo-%02d", i))
			p.Begin(tg)
			p.Finish(pullsvc.Result{Target: tg})
		})
	}
	wg.Wait()
	p.Close()

	if got := screenLines(t, out.String()); len(got) != 0 {
		t.Fatalf("screen should be empty with nothing committed, got %q", got)
	}
	if p.finished != 50 || len(p.active) != 0 {
		t.Fatalf("finished = %d, active = %d; want 50 and 0", p.finished, len(p.active))
	}
}
