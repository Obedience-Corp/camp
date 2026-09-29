package fresh

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/ui"
)

func TestFreshLivePipePrintsOneSettledRow(t *testing.T) {
	var buf bytes.Buffer
	step := newFreshLive(&buf, "Fetch origin", "fetching", "")
	step.animated = false
	step.width = 80
	step.Begin()
	step.Finish("done", ui.StatusSuccess)
	if strings.Contains(buf.String(), "\x1b[1A") {
		t.Fatalf("pipe output moved the cursor:\n%s", buf.String())
	}
	plain := stripFreshANSI(applyFreshTerm(buf.String()))
	if strings.Count(plain, "Fetch origin") != 1 || !strings.Contains(plain, "done") {
		t.Fatalf("settled row:\n%s", plain)
	}
	if strings.Contains(plain, "fetching") {
		t.Fatalf("pipe kept the working status:\n%s", plain)
	}
}

func TestFreshLiveSpinnerSettlesToTheResult(t *testing.T) {
	var buf bytes.Buffer
	step := newFreshLive(&buf, "Fetch origin", "fetching", "")
	step.animated = true
	step.width = 80
	step.mu.Lock()
	step.paintLocked()
	step.frame++
	step.paintLocked()
	step.mu.Unlock()
	if !strings.Contains(buf.String(), "\x1b[1A\x1b[2K") {
		t.Fatal("second frame did not erase the first")
	}
	step.Finish("done", ui.StatusSuccess)
	plain := stripFreshANSI(applyFreshTerm(buf.String()))
	if strings.Contains(plain, "fetching") || strings.ContainsAny(plain, strings.Join(freshSpinnerFrames, "")) {
		t.Fatalf("spinner stayed on screen:\n%s", plain)
	}
	if !strings.Contains(plain, "done") || strings.Count(plain, "Fetch origin") != 1 {
		t.Fatalf("settled row:\n%s", plain)
	}
}

func TestFreshLiveOutputKeepsTheWorkingRow(t *testing.T) {
	var buf bytes.Buffer
	step := newFreshLive(&buf, "install", "running", "$ printf")
	step.animated = true
	step.width = 80
	step.Begin()
	time.Sleep(2 * freshSpinnerTick)
	if _, err := step.Stream().Write([]byte("built camp\n")); err != nil {
		t.Fatal(err)
	}
	step.Finish("done", ui.StatusSuccess)
	plain := stripFreshANSI(applyFreshTerm(buf.String()))
	for _, want := range []string{"install", "$ printf", "built camp", "done"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "running") || strings.ContainsAny(plain, strings.Join(freshSpinnerFrames, "")) {
		t.Fatalf("working row stayed on screen:\n%s", plain)
	}
}

// applyFreshTerm applies the cursor-up and erase-line sequences this spinner writes.
func applyFreshTerm(s string) string {
	var lines []string
	var cur strings.Builder
	flush := func() {
		lines = append(lines, cur.String())
		cur.Reset()
	}
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "\x1b[1A") {
			if cur.Len() == 0 && len(lines) > 0 {
				lines = lines[:len(lines)-1]
			}
			i += len("\x1b[1A")
			continue
		}
		if strings.HasPrefix(s[i:], "\x1b[2K") {
			cur.Reset()
			i += len("\x1b[2K")
			continue
		}
		if s[i] == '\n' {
			flush()
			i++
			continue
		}
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		cur.WriteByte(s[i])
		i++
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return strings.Join(lines, "\n")
}

func stripFreshANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		for i < len(s) && s[i] != 'm' {
			i++
		}
	}
	return b.String()
}
