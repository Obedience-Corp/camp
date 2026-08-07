package artifacts

import (
	"github.com/Obedience-Corp/camp/internal/rsyncprobe"
)

// Two transfer engines, one staging contract.
//
// A pull is: build the local manifest, work out which files are protected,
// stage the peer's candidates outside the live root, then merge each staged
// file only if the destination still matches the agreed baseline. Only the
// staging step differs between a delta transfer and a whole-file one —
// everything that decides what is safe to overwrite runs afterwards and is
// shared. That is what makes "identical from the user's view except speed"
// structural rather than a promise: the conflict posture, the snapshot, and
// the JSON shape are produced by code neither engine can vary.
//
// The engine is chosen from the probe verdict (D002), never from a flag or an
// OS guess, and the result records which one ran and why.

// Engine names as they appear in `--json`. Stable strings: scripts branch on
// them.
const (
	// EngineDelta is genuine rsync's delta transfer, used when both ends are
	// genuine rsync at protocol >= 30.
	EngineDelta = "rsync-delta"
	// EngineWholeFile sends each changed file in full. Used when either end
	// cannot be trusted to do a delta: openrsync, old protocols, or anything
	// camp could not identify.
	EngineWholeFile = "whole-file"
)

// transferEngine is the staging half of a pull: the part that differs.
type transferEngine struct {
	// name is the reported engine (EngineDelta or EngineWholeFile).
	name string
	// reason explains a non-delta choice; empty for the delta engine.
	reason string
	// wholeFileFlag adds rsync's -W when the resolved binary is genuine rsync.
	// openrsync has no --whole-file because it never implements the delta
	// algorithm — it is whole-file already — so camp does not pass a flag that
	// binary does not document, and the transfer is whole-file either way.
	wholeFileFlag bool
}

// engineFor picks the transfer engine from a probe verdict.
//
// A pair that could not be probed at all is treated exactly like an unusable
// one: the safe direction is the honest slower transfer, never an optimistic
// delta against an engine camp knows nothing about.
func engineFor(pair rsyncprobe.Pair, probeErr error) transferEngine {
	if probeErr != nil {
		return transferEngine{
			name:   EngineWholeFile,
			reason: "rsync probe failed: " + probeErr.Error(),
		}
	}
	if pair.DeltaUsable() {
		return transferEngine{name: EngineDelta}
	}
	return transferEngine{
		name:          EngineWholeFile,
		reason:        pair.Reason(),
		wholeFileFlag: pair.Local.Kind == rsyncprobe.KindRsync,
	}
}

// stagingArgs returns the engine-specific rsync arguments, prepended to the
// options both engines share. partialDir must be an absolute path OUTSIDE the
// staging tree.
//
// The whole-file engine keeps --compare-dest: that is what skips files the
// local root already has, and it is change detection, not delta transfer. What
// -W removes is the block-level algorithm *within* a file that changed. Adding
// --partial-dir keeps an interrupted file so a retry resumes it rather than
// restarting — resumable at file granularity, which is exactly what D002
// promises for this path and all a whole-file transfer can honestly offer.
//
// The partial directory is deliberately not inside the staging tree. The merge
// step walks every regular file it finds there and moves it into the live
// root; an in-flight partial sitting under staging would be merged as though
// it were a complete artifact, which is a truncated file presented as real
// data. Keeping it outside means the staging tree only ever holds whole files.
func (e transferEngine) stagingArgs(partialDir string) []string {
	if e.name != EngineWholeFile {
		return nil
	}
	args := []string{"--partial", "--partial-dir=" + partialDir}
	if e.wholeFileFlag {
		args = append(args, "-W")
	}
	return args
}
