package jobs

import (
	"os"
	"path/filepath"
	"time"
)

// When the attempt in front of you started.
//
// The queue's own state is a directory name, which answers "is this job
// running" but not "for how long". A user looking at a running job cannot tell
// a commit message being written from one that stopped being written forty
// minutes ago, and those need opposite responses.
//
// The signal is the running file's own mtime, set at the moment of the claim.
// A field in the document would be the explicit form and is the wrong one
// here: rewriting a file in running/ is the single edit this queue refuses to
// make, because a crash mid-write leaves JSON that reclaim cannot parse and
// therefore never picks up again. A timestamp is metadata, so setting it
// touches no bytes and can leave nothing half-written.

// markAttemptStart records that this attempt begins now.
//
// Best effort. A filesystem that will not set times leaves the file's write
// time in place, so elapsed reads as too long rather than too short: the
// listing over-reports a stall it should have under-reported, which sends a
// user to look at a healthy job instead of leaving them ignorant of a sick one.
func markAttemptStart(runningPath string) {
	now := time.Now()
	_ = os.Chtimes(runningPath, now, now)
}

// attemptStart reports when the current attempt at a running job began, and
// whether that is known at all.
//
// Unknown rather than zero-valued when the file is gone: a job completed
// between the listing's read and this stat is finished, not instantaneous, and
// a caller that saw a duration of zero would render it as one.
func attemptStart(campaignRoot, repo string, seq int) (time.Time, bool) {
	info, err := os.Stat(runningJobPath(campaignRoot, repo, seq))
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// runningJobPath is where a claimed job's file lives.
//
// Derived from the sequence rather than carried around, because the filename
// is a function of the sequence alone; that is the same property that makes
// O_CREATE|O_EXCL a working collision check at enqueue.
func runningJobPath(campaignRoot, repo string, seq int) string {
	return filepath.Join(laneDir(campaignRoot, stateRunning, repo), jobFilename(seq))
}
