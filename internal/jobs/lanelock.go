package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Lane-lock timing.
//
// These are deliberately not fsutil.AcquireFileLock's constants. That lock
// guards short registry and link writes, and its own comment says a
// long-running holder should carry a separate threshold rather than stretch
// the 30s global. A worker holds its lane for as long as the jobs take, which
// can include an LLM call for a deferred message, so it needs a liveness
// signal rather than a timeout: it heartbeats, and a lock that stops
// heartbeating is genuinely abandoned.
//
// Both are variables rather than constants so tests can drive time instead of
// sleeping through it.
var (
	// heartbeatEvery is how often a working lane refreshes its lock mtime.
	heartbeatEvery = 5 * time.Second
	// laneLiveness is the age past which a lock is presumed abandoned. Well
	// above heartbeatEvery so an ordinary scheduling delay is never mistaken
	// for a dead worker.
	laneLiveness = 60 * time.Second
	// laneCap bounds workers running at once. A campaign with 37 submodules
	// must not spawn 37 workers; the cap also bounds concurrent message
	// writers once deferred --auto-write lands.
	laneCap = 4
)

// laneLockName is the lock filename for a lane slug.
func laneLockName(slug string) string {
	return "worker-" + slug + ".lock"
}

// laneLock is a held lane, kept alive by a heartbeat goroutine.
type laneLock struct {
	path string
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// acquireLane takes the lock for a lane.
//
// It returns (nil, false, nil) when a live worker holds the lane: that is
// ordinary contention, not an error, and the caller should simply leave the
// lane alone. A lock whose mtime is older than laneLiveness is presumed
// abandoned and stolen once.
func acquireLane(queueDir, slug string) (*laneLock, bool, error) {
	if err := os.MkdirAll(queueDir, 0o755); err != nil {
		return nil, false, camperrors.Wrapf(err, "create queue dir %s", queueDir)
	}
	path := filepath.Join(queueDir, laneLockName(slug))

	// Two attempts: the second exists so a stolen stale lock can be retaken
	// without recursing. More would mean fighting a live competitor.
	for range 2 {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			l := &laneLock{path: path, stop: make(chan struct{}), done: make(chan struct{})}
			go l.heartbeat()
			return l, true, nil
		}
		if !os.IsExist(err) {
			return nil, false, camperrors.Wrapf(err, "acquire lane lock %s", path)
		}

		info, statErr := os.Stat(path)
		if statErr != nil {
			continue // the holder released between our create and our stat
		}
		if time.Since(info.ModTime()) < laneLiveness {
			return nil, false, nil // a live worker owns this lane
		}
		_ = os.Remove(path) // abandoned: steal it and retry
	}
	return nil, false, nil
}

// heartbeat refreshes the lock mtime so other processes can tell this worker
// apart from a crashed one.
func (l *laneLock) heartbeat() {
	defer close(l.done)
	t := time.NewTicker(heartbeatEvery)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			now := time.Now()
			_ = os.Chtimes(l.path, now, now)
		case <-l.stop:
			return
		}
	}
}

// release stops the heartbeat and removes the lock. Safe to call more than
// once so a deferred release cannot panic on an already-released lane.
func (l *laneLock) release() {
	l.once.Do(func() {
		close(l.stop)
		<-l.done
		_ = os.Remove(l.path)
	})
}

// laneLockFresh reports whether a lane is held by a worker that is still
// heartbeating. Used by the spawn path to avoid starting a second worker for a
// lane that already has one.
func laneLockFresh(queueDir, slug string) bool {
	info, err := os.Stat(filepath.Join(queueDir, laneLockName(slug)))
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < laneLiveness
}

// countFreshLaneLocks returns how many lanes are currently held by live
// workers, across processes.
//
// This is the cross-process half of the cap. The in-process semaphore alone
// cannot see other workers, and the lock count alone races with a worker that
// has decided to start but not yet created its lock; together they close the
// window in the direction that matters, since over-spawning is benign (the
// loser's acquireLane returns !ok and it exits) while under-serving is not.
func countFreshLaneLocks(queueDir string) int {
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		return 0
	}
	n := 0
	cutoff := time.Now().Add(-laneLiveness)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !isLaneLockName(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			n++
		}
	}
	return n
}

func isLaneLockName(name string) bool {
	return len(name) > len("worker-.lock") &&
		name[:len("worker-")] == "worker-" &&
		name[len(name)-len(".lock"):] == ".lock"
}
