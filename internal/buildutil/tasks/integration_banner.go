// internal/buildutil/tasks/integration_banner.go
package tasks

import (
	"bytes"
	"strings"
	"sync"
)

// maxBannerLine bounds the unterminated tail bannerWatcher will hold, so a
// suite that prints a very long line without a newline cannot grow it without
// limit over a twelve minute run.
const maxBannerLine = 8192

// bannerWatcher tees a stream while watching for the harness's package-level
// infrastructure banner.
//
// It matches only at the start of a line. `go test -v` indents everything a
// test prints, so an indented banner belongs to one test's failure (a single
// member-local fault the run can survive), while a banner at column zero is
// the harness saying the run itself did not happen. Treating the two alike
// would turn one unlucky container into a non-run.
type bannerWatcher struct {
	mu      sync.Mutex
	partial []byte
	reason  string
}

func (w *bannerWatcher) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.reason != "" {
		return len(p), nil
	}
	w.partial = append(w.partial, p...)
	for {
		end := bytes.IndexByte(w.partial, '\n')
		if end < 0 {
			break
		}
		line := string(w.partial[:end])
		w.partial = w.partial[end+1:]
		if strings.HasPrefix(line, infraBannerMarker) {
			w.reason = strings.TrimSpace(line)
			w.partial = nil
			return len(p), nil
		}
	}
	if len(w.partial) > maxBannerLine {
		// Keep the head: the banner starts its line, so that is the part worth
		// holding on to.
		w.partial = w.partial[:maxBannerLine]
	}
	return len(p), nil
}

// refusal reports the banner line, if the stream carried one.
func (w *bannerWatcher) refusal() (bool, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reason != "", w.reason
}
