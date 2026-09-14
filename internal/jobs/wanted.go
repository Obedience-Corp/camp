package jobs

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// Whether anyone is blocked on a lane right now.
//
// A worker waiting out an unavailable message writer is spending time that
// belongs to nobody: the terminal returned long ago and the captured tree is
// not going anywhere. That stops being true the instant a command blocks on
// the same lane, and then the trade reverses completely — a better commit
// subject is not worth a person watching a spinner, which is the whole reason
// this queue exists.
//
// The signal is a marker file per lane, refreshed by a drain while it waits
// and read by the worker while it sleeps. Mtime and nothing else, the same
// liveness idiom as the lane lock: a marker is "someone is waiting" only while
// it is fresh, so a stale one from yesterday's drain says nothing and there is
// no cleanup to get wrong.

// wantedFresh is how recently a lane must have been marked for a worker to
// treat someone as blocked on it. Comfortably above wantedMarkEvery so an
// ordinary scheduling delay inside a waiting drain is not read as the drain
// having gone away.
var wantedFresh = 3 * time.Second

// wantedMarkEvery throttles a waiting drain's refresh. The drain polls the
// queue several times a second and the marker only has to stay fresh.
var wantedMarkEvery = time.Second

// laneWantedName is the marker filename for a lane slug. It shares the queue
// directory with the lane locks and is deliberately not named like one:
// countFreshLaneLocks reads that directory and must not count a marker as a
// worker.
func laneWantedName(slug string) string {
	return "wanted-" + slug + ".mark"
}

// markLaneWanted records that something is blocked on this lane now.
//
// Best effort, like every other signal in this queue that only ever makes a
// worker do the older, safer thing: a marker that cannot be written costs a
// commit its wait, not its message.
func markLaneWanted(queueDir, slug string) {
	if err := os.MkdirAll(queueDir, 0o755); err != nil {
		return
	}
	path := filepath.Join(queueDir, laneWantedName(slug))
	now := time.Now()
	if err := os.Chtimes(path, now, now); err == nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_ = f.Close()
}

// MarkWanted records that the caller is blocked on repo's lane, so a worker
// waiting for an unavailable message writer stops waiting and lands the
// commit.
//
// Exported for the drain, which is the only thing that can know a command is
// on the critical path for a lane. Reporting commands do not call it: they say
// what is outstanding and carry on, so nothing about them is worth a worse
// commit message.
func MarkWanted(campaignRoot string, blocking []Job) {
	if campaignRoot == "" {
		return
	}
	queueDir := QueueDir(campaignRoot)
	seen := make(map[string]struct{}, len(blocking))
	for _, job := range blocking {
		lane := normalizeRepo(job.Repo)
		if _, ok := seen[lane]; ok {
			continue
		}
		seen[lane] = struct{}{}
		markLaneWanted(queueDir, LaneSlug(lane))
	}
}

// KeepWanted marks every lane with outstanding work as wanted until the
// returned stop function is called, and returns a no-op outside a campaign.
//
// For the foreground `camp jobs run`, which is a person asking camp to finish
// the queue while they watch. A worker serving that request must not stop to
// wait an hour for a better commit message, and the marker has a liveness
// window rather than a flag precisely so this can be expressed by refreshing
// it for exactly as long as somebody is there.
//
// The detached worker is the opposite case and deliberately does not call it:
// nobody is watching that one, which is the whole reason it is allowed to wait
// at all.
func KeepWanted(ctx context.Context, campaignRoot string) func() {
	if campaignRoot == "" {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		t := time.NewTicker(wantedMarkEvery)
		defer t.Stop()
		for {
			if blocking, err := OutstandingAll(campaignRoot); err == nil {
				MarkWanted(campaignRoot, blocking)
			}
			select {
			case <-t.C:
			case <-ctx.Done():
				return
			case <-done:
				return
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

// laneWanted reports whether something is blocked on this lane right now.
func laneWanted(queueDir, slug string) bool {
	info, err := os.Stat(filepath.Join(queueDir, laneWantedName(slug)))
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < wantedFresh
}
