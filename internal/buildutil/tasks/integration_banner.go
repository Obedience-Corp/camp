// internal/buildutil/tasks/integration_banner.go
package tasks

import (
	"bytes"
	"strings"
	"sync"
)

// maxBannerLine bounds the unterminated tail bannerWatcher holds.
const maxBannerLine = 8192

// bannerWatcher tees a stream while watching for the harness's package-level
// infrastructure banner. It matches at column zero only: go test indents what
// a test prints, and one test's infra fault is not a non-run.
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
		// The banner starts its line, so the head is the part worth keeping.
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
