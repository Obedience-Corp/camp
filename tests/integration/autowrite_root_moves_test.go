//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trackNoteScript commits a multi-line note outside the root, so an edit
// after a move stays well above git's rename similarity threshold.
const trackNoteScript = `
		mkdir -p notes
		seq 1 40 | sed 's/^/carry me /' > notes/readme.md
		git add notes/readme.md
		git -c user.email=t@t -c user.name=t commit -q -m "track the note"
`

// commitRenameIntoRoot checks that a rename or move lands as the new path in
// place of the old one, with nothing left staged or untracked for either.
// Inside a root, size decides, so the new path is committed whatever its
// name or contents.
func commitRenameIntoRoot(t *testing.T, name string, autoWrite bool, prepare, src, dst string) {
	t.Helper()
	tc, campPath, _ := rootCommit(t, name, autoWrite, prepare)
	assertInHEAD(t, tc, campPath, []string{dst}, []string{src})
	assertOnlyRootMediaLeft(t, tc, campPath, "videos/my-video/footage.mp4")
}

func TestIntegration_MoveIntoDeclaredRootIsCommitted(t *testing.T) {
	commitRenameIntoRoot(t, "move-into-root", false,
		trackNoteScript+"mv notes/readme.md videos/my-video/readme.md\n",
		"notes/readme.md", "videos/my-video/readme.md")
}

func TestIntegration_AutoWriteMoveIntoDeclaredRootIsCommitted(t *testing.T) {
	commitRenameIntoRoot(t, "aw-move-into-root", true,
		trackNoteScript+"mv notes/readme.md videos/my-video/readme.md\n",
		"notes/readme.md", "videos/my-video/readme.md")
}

func TestIntegration_RenameInsideDeclaredRootIsCommitted(t *testing.T) {
	commitRenameIntoRoot(t, "rename-in-root", false,
		"mv videos/my-video/script.md videos/my-video/shot-list.md\n",
		"videos/my-video/script.md", "videos/my-video/shot-list.md")
}

func TestIntegration_AutoWriteRenameInsideDeclaredRootIsCommitted(t *testing.T) {
	commitRenameIntoRoot(t, "aw-rename-in-root", true,
		"mv videos/my-video/script.md videos/my-video/shot-list.md\n",
		"videos/my-video/script.md", "videos/my-video/shot-list.md")
}

// The destination is a new directory inside the root, which the collapsed
// untracked listing reports as one entry and the root plan expands.
const editedMoveScript = `
		mkdir -p videos/my-video/drafts
		mv notes/readme.md videos/my-video/drafts/readme.md
		printf 'edited after the move\n' >> videos/my-video/drafts/readme.md
`

func TestIntegration_EditedMoveIntoDeclaredRootIsCommitted(t *testing.T) {
	commitRenameIntoRoot(t, "edited-move-into-root", false,
		trackNoteScript+editedMoveScript,
		"notes/readme.md", "videos/my-video/drafts/readme.md")
}

func TestIntegration_AutoWriteEditedMoveIntoDeclaredRootIsCommitted(t *testing.T) {
	commitRenameIntoRoot(t, "aw-edited-move-into-root", true,
		trackNoteScript+editedMoveScript,
		"notes/readme.md", "videos/my-video/drafts/readme.md")
}

// A rename outside the root next to a near-copy inside it commits the same
// files on both paths: both use one listing and one size rule, so neither
// can pair the deleted path with a different destination.
const nearCopyScript = `
		mkdir -p notes
		seq 1 40 | sed 's/^/carry me /' > notes/a.md
		git add notes/a.md
		git -c user.email=t@t -c user.name=t commit -q -m "track a"
		mv notes/a.md notes/b.md
		cp notes/b.md videos/my-video/a-copy.md
		printf 'one more line\n' >> videos/my-video/a-copy.md
`

func TestIntegration_NearCopyBesideARenameCommitsTheSameFiles(t *testing.T) {
	tc, campPath, _ := rootCommit(t, "near-copy", false, nearCopyScript)
	assertInHEAD(t, tc, campPath, []string{"notes/b.md", "videos/my-video/a-copy.md"}, []string{"notes/a.md"})
	assertOnlyRootMediaLeft(t, tc, campPath, "videos/my-video/footage.mp4")
}

func TestIntegration_AutoWriteNearCopyBesideARenameCommitsTheSameFiles(t *testing.T) {
	tc, campPath, _ := rootCommit(t, "aw-near-copy", true, nearCopyScript)
	assertInHEAD(t, tc, campPath, []string{"notes/b.md", "videos/my-video/a-copy.md"}, []string{"notes/a.md"})
	assertOnlyRootMediaLeft(t, tc, campPath, "videos/my-video/footage.mp4")
}

// A tracked file over the size limit that moves into a root, or is renamed
// inside one, is kept out like the root's media, and git records its old
// path as deleted. That is reported on its own, naming both paths, with the
// command that keeps it tracked, never folded into the media tally.
func commitOverLimitMove(t *testing.T, name string, autoWrite bool, track, move, src, dst string) {
	t.Helper()
	prepare := fmt.Sprintf(`
		mkdir -p "$(dirname %[1]s)"
		dd if=/dev/urandom of=%[1]s bs=1024 count=2048 2>/dev/null
		git add -- %[1]s
		git -c user.email=t@t -c user.name=t commit -q -m "track the large file"
		mkdir -p "$(dirname %[2]s)"
		%[3]s
	`, track, dst, move)
	tc, campPath, output := rootCommit(t, name, autoWrite, prepare)
	t.Logf("commit output:\n%s", output)

	assert.Contains(t, output, src+" was tracked and is now "+dst, "output:\n%s", output)
	assert.Contains(t, output, "git records "+src+" as deleted", "output:\n%s", output)
	assert.Contains(t, output, "Keep it tracked:  git add -- "+dst, "output:\n%s", output)
	assert.Contains(t, output, "videos/my-video/ +1 files (3.0 MB) kept out of git",
		"the moved file must not be counted with the root's media; output:\n%s", output)
	assertInHEAD(t, tc, campPath, nil, []string{src, dst})
	assertOnlyRootMediaLeft(t, tc, campPath, "videos/my-video/footage.mp4", dst)

	tc.Shell(t, fmt.Sprintf("set -e\ncd %s\ngit add -- %s", campPath, dst))
	undo, err := tc.RunCampInDir(campPath, "commit", "-m", "keep it tracked")
	require.NoError(t, err, "the printed command must keep the file tracked; output:\n%s", undo)
	settleQueue(t, tc, campPath)
	assertInHEAD(t, tc, campPath, []string{dst}, []string{src})
	assertOnlyRootMediaLeft(t, tc, campPath, "videos/my-video/footage.mp4")
}

func TestIntegration_OverLimitMoveIntoDeclaredRootIsReported(t *testing.T) {
	commitOverLimitMove(t, "over-limit-move", false,
		"notes/model.bin", "mv notes/model.bin videos/my-video/model.bin",
		"notes/model.bin", "videos/my-video/model.bin")
}

func TestIntegration_AutoWriteOverLimitMoveIntoDeclaredRootIsReported(t *testing.T) {
	commitOverLimitMove(t, "aw-over-limit-move", true,
		"notes/model.bin", "mv notes/model.bin videos/my-video/model.bin",
		"notes/model.bin", "videos/my-video/model.bin")
}

func TestIntegration_OverLimitRenameInsideDeclaredRootIsReported(t *testing.T) {
	commitOverLimitMove(t, "over-limit-rename", false,
		"videos/my-video/take.mov", "mv videos/my-video/take.mov videos/my-video/take-final.mov",
		"videos/my-video/take.mov", "videos/my-video/take-final.mov")
}

func TestIntegration_AutoWriteOverLimitRenameInsideDeclaredRootIsReported(t *testing.T) {
	commitOverLimitMove(t, "aw-over-limit-rename", true,
		"videos/my-video/take.mov", "mv videos/my-video/take.mov videos/my-video/take-final.mov",
		"videos/my-video/take.mov", "videos/my-video/take-final.mov")
}
