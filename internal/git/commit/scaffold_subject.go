package commit

import (
	"slices"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/git"
)

const (
	initAction    = "Init"
	initSubject   = "scaffold camp workspace"
	repairAction  = "Repair"
	repairSubject = "camp repair"
	// legacyRepairSubject is what repair commits said before the camp rename.
	legacyRepairSubject = "campaign repair"

	// selectiveRepairCutoff is the committer date of 4d5f046d, which made
	// repair stage only the files it wrote. Before it, a repair commit staged
	// the whole tree and could carry a user's pending edit.
	selectiveRepairCutoff = "2026-03-03T15:28:35-07:00"
)

var selectiveRepairSince, _ = time.Parse(time.RFC3339, selectiveRepairCutoff)

var repairSubjects = []string{
	repairAction + ": " + repairSubject,
	repairAction + ": " + legacyRepairSubject,
}

// IsScaffoldCommit reports whether subject, committed at committed, is a
// commit camp init or camp repair made under its context tag, in either the
// name-style or the legacy tag form. Those commands write scaffold files and
// commit only those files in the same process, so each file such a commit adds
// holds what camp wrote. Init always committed that way; repair did from
// selectiveRepairCutoff on, so an earlier repair commit does not count.
func IsScaffoldCommit(subject string, committed time.Time) bool {
	if git.ParseTag(subject).CampaignID == "" {
		return false
	}
	_, rest, _ := strings.Cut(subject, "]")
	rest = strings.TrimPrefix(rest, " ")
	if rest == initAction+": "+initSubject {
		return true
	}
	return slices.Contains(repairSubjects, rest) && !committed.Before(selectiveRepairSince)
}
