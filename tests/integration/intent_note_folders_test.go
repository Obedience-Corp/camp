//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntentNoteFolders_CRUDAndImportMeetingJSON exercises note folder
// create/list/move/rm and import-meeting through the real camp binary inside
// the container harness (not the explorer key handlers).
func TestIntentNoteFolders_CRUDAndImportMeetingJSON(t *testing.T) {
	tc := GetSharedContainer(t)

	const campPath = "/campaigns/note-folders-ci0009"
	_, err := tc.InitCampaign(campPath, "note-folders-ci0009", "product")
	require.NoError(t, err)

	// Create nested folder; must write .gitkeep.
	out, err := tc.RunCampInDir(campPath, "idea", "notes", "folders", "add", "reading/papers")
	require.NoError(t, err, "folders add: %s", out)
	assert.Contains(t, out, "notes/reading/papers")

	exists, err := tc.CheckFileExists(campPath + "/.campaign/intents/notes/reading/papers/.gitkeep")
	require.NoError(t, err)
	assert.True(t, exists, ".gitkeep should exist after folders add")

	// JSON list contract.
	jsonOut, err := tc.RunCampInDir(campPath, "idea", "notes", "folders", "--json")
	require.NoError(t, err, "folders --json: %s", jsonOut)
	assert.Contains(t, jsonOut, `"schema_version": "intent-note-folders/v1alpha1"`)
	assert.Contains(t, jsonOut, `"status": "notes/reading/papers"`)
	assert.NotContains(t, jsonOut, `"folders": null`)

	// Rename folder (directory move only).
	out, err = tc.RunCampInDir(campPath, "idea", "notes", "folders", "mv", "reading/papers", "reading/books")
	require.NoError(t, err, "folders mv: %s", out)
	exists, err = tc.CheckFileExists(campPath + "/.campaign/intents/notes/reading/books/.gitkeep")
	require.NoError(t, err)
	assert.True(t, exists, "renamed folder should exist")

	// Capture a note into the folder, then move it to root.
	out, err = tc.RunCampInDir(campPath, "idea", "note", "paper note",
		"--folder", "reading/books", "--no-commit")
	require.NoError(t, err, "note --folder: %s", out)
	assert.Contains(t, out, "notes/reading/books/")

	// The note-create command has no JSON mode and lifecycle list/find omit
	// notes. Resolve the ID from the exact folder contract rather than parsing
	// human success text.
	id := strings.TrimSpace(tc.Shell(t, fmt.Sprintf(
		"basename \"$(find %s/.campaign/intents/notes/reading/books -maxdepth 1 -type f -name '*.md' | head -n 1)\" .md",
		campPath,
	)))
	require.NotEmpty(t, id)

	out, err = tc.RunCampInDir(campPath, "idea", "notes", "mv", id, ".")
	require.NoError(t, err, "notes mv: %s", out)

	// Delete empty folder.
	out, err = tc.RunCampInDir(campPath, "idea", "notes", "folders", "rm", "reading/books")
	require.NoError(t, err, "folders rm: %s", out)

	// Reserved name rejected.
	out, err = tc.RunCampInDir(campPath, "idea", "notes", "folders", "add", "meetings")
	require.Error(t, err, "reserved meetings should fail: %s", out)

	// import-meeting: create a minimal bundle in the container.
	bundle := "/tmp/ci0009-standup.meeting"
	tc.Shell(t, fmt.Sprintf("mkdir -p %s && printf '# Meeting: standup\\n' > %s/meeting.md", bundle, bundle))
	out, err = tc.RunCampInDir(campPath, "idea", "notes", "import-meeting", bundle,
		"--summary", "## Summary\n\nStandup notes.\n", "--json", "--author", "agent")
	require.NoError(t, err, "import-meeting: %s", out)
	assert.Contains(t, out, `"schema_version": "intent-meeting-import/v1alpha1"`)
	assert.Contains(t, out, "notes/meetings/")
	assert.Contains(t, out, ".transcripts/")
}
