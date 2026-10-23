package commit

import (
	"slices"
	"strings"

	"github.com/Obedience-Corp/camp/internal/git"
)

const (
	initAction    = "Init"
	initSubject   = "scaffold camp workspace"
	repairAction  = "Repair"
	repairSubject = "camp repair"
	// legacyRepairSubject is what repair commits said before the camp rename.
	legacyRepairSubject = "campaign repair"
)

var scaffoldSubjects = []string{
	initAction + ": " + initSubject,
	repairAction + ": " + repairSubject,
	repairAction + ": " + legacyRepairSubject,
}

// IsScaffoldCommit reports whether subject is a commit camp init or camp
// repair made under its context tag, in either the name-style or the legacy
// tag form. Those commands write scaffold files and commit them in the same
// process, so each file such a commit adds holds what camp wrote.
func IsScaffoldCommit(subject string) bool {
	if git.ParseTag(subject).CampaignID == "" {
		return false
	}
	_, rest, _ := strings.Cut(subject, "]")
	return slices.Contains(scaffoldSubjects, strings.TrimPrefix(rest, " "))
}
