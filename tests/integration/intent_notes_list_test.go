//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type notesListItem struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	Folder    *string  `json:"folder"`
	Author    string   `json:"author"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	Path      string   `json:"path"`
}

type notesListPayload struct {
	SchemaVersion string          `json:"schema_version"`
	CampaignRoot  string          `json:"campaign_root"`
	Items         []notesListItem `json:"items"`
}

type noteAddPayload struct {
	SchemaVersion string `json:"schema_version"`
	CampaignRoot  string `json:"campaign_root"`
	ID            string `json:"id"`
	Path          string `json:"path"`
}

func runCampJSONInDir(t *testing.T, tc *TestContainer, dir string, out any, args ...string) {
	t.Helper()
	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(dir, args...)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "camp %v failed\nstdout=%s\nstderr=%s", args, stdout, stderr)
	require.NoError(t, json.Unmarshal([]byte(stdout), out), "camp %v stdout is not one JSON document:\n%s", args, stdout)
}

func notesListIDs(items []notesListItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func notesListFolders(items []notesListItem) map[string]string {
	folders := make(map[string]string, len(items))
	for _, item := range items {
		if item.Folder != nil {
			folders[item.ID] = *item.Folder
		}
	}
	return folders
}

// TestIntentNotesList_FoldersSortAndJSONContract drives note capture with
// --json, notes list with and without --folder, and idea list --status notes
// through the real camp binary inside the container harness.
func TestIntentNotesList_FoldersSortAndJSONContract(t *testing.T) {
	tc := GetSharedContainer(t)

	const campPath = "/campaigns/notes-list-json"
	_, err := tc.InitCampaign(campPath, "notes-list-json", "product")
	require.NoError(t, err)
	notesDir := campPath + "/.campaign/intents/notes"

	out, err := tc.RunCampInDir(campPath, "idea", "notes", "folders", "add", "reading/papers")
	require.NoError(t, err, "folders add: %s", out)

	var rootAdd noteAddPayload
	runCampJSONInDir(t, tc, campPath, &rootAdd, "idea", "note", "root note", "--json", "--no-commit")
	assert.Equal(t, "intents/v1alpha1", rootAdd.SchemaVersion)
	require.NotEmpty(t, rootAdd.ID)
	assert.True(t, strings.HasPrefix(rootAdd.Path, ".campaign/intents/notes/"), "note add path = %q", rootAdd.Path)
	exists, err := tc.CheckFileExists(rootAdd.CampaignRoot + "/" + rootAdd.Path)
	require.NoError(t, err)
	assert.True(t, exists, "note add path must join against campaign_root")

	var paperAdd noteAddPayload
	runCampJSONInDir(t, tc, campPath, &paperAdd, "idea", "note", "paper note",
		"--folder", "reading/papers", "--tag", "research", "--json", "--no-commit")
	assert.True(t, strings.HasPrefix(paperAdd.Path, ".campaign/intents/notes/reading/papers/"), "paper path = %q", paperAdd.Path)

	var ideaAdd noteAddPayload
	runCampJSONInDir(t, tc, campPath, &ideaAdd, "idea", "add", "lifecycle idea", "--json", "--no-commit")

	const meetingID = "old-meeting-20250101-090000"
	require.NoError(t, tc.WriteFile(notesDir+"/meetings/"+meetingID+".md", fmt.Sprintf(
		"---\nid: %s\ntitle: Old meeting\nstatus: notes/meetings\ncreated_at: 2025-01-01T09:00:00Z\nauthor: agent\ntags: []\n---\n\n# Old meeting\n",
		meetingID)))
	const archivedID = "shelved-note-20250102-090000"
	require.NoError(t, tc.WriteFile(notesDir+"/archived/"+archivedID+".md", fmt.Sprintf(
		"---\nid: %s\ntitle: Shelved note\nstatus: notes/archived\ncreated_at: 2025-01-02T09:00:00Z\nauthor: agent\ntags: []\n---\n",
		archivedID)))

	var all notesListPayload
	runCampJSONInDir(t, tc, campPath, &all, "idea", "notes", "list", "--json")
	assert.Equal(t, "intents/v1alpha1", all.SchemaVersion)
	require.Len(t, all.Items, 3, "default list covers root, folders, and meetings but not archived: %v", notesListIDs(all.Items))
	assert.ElementsMatch(t, []string{rootAdd.ID, paperAdd.ID, meetingID}, notesListIDs(all.Items))
	assert.Equal(t, meetingID, all.Items[2].ID, "oldest created_at sorts last")
	for i := 1; i < len(all.Items); i++ {
		prev, err := time.Parse(time.RFC3339, all.Items[i-1].CreatedAt)
		require.NoError(t, err)
		cur, err := time.Parse(time.RFC3339, all.Items[i].CreatedAt)
		require.NoError(t, err)
		assert.False(t, prev.Before(cur), "items must be newest first by created_at: %s before %s", prev, cur)
	}
	assert.Equal(t, map[string]string{rootAdd.ID: "", paperAdd.ID: "reading/papers", meetingID: "meetings"},
		notesListFolders(all.Items), "every note item carries folder, empty for the root")
	for _, item := range all.Items {
		assert.Equal(t, "notes", item.Status, "item %s status", item.ID)
		assert.Equal(t, "agent", item.Author, "item %s author", item.ID)
		assert.NotEmpty(t, item.CreatedAt, "item %s created_at", item.ID)
		exists, err := tc.CheckFileExists(all.CampaignRoot + "/" + item.Path)
		require.NoError(t, err)
		assert.True(t, exists, "item %s path %q must join against campaign_root", item.ID, item.Path)
		if item.ID == paperAdd.ID {
			assert.Equal(t, []string{"research"}, item.Tags)
		}
	}

	folderCases := []struct {
		folder string
		want   []string
	}{
		{folder: "reading/papers", want: []string{paperAdd.ID}},
		{folder: "reading", want: []string{}},
		{folder: ".", want: []string{rootAdd.ID}},
		{folder: "meetings", want: []string{meetingID}},
		{folder: "archived", want: []string{archivedID}},
	}
	for _, fc := range folderCases {
		var filtered notesListPayload
		runCampJSONInDir(t, tc, campPath, &filtered, "idea", "notes", "list", "--folder", fc.folder, "--json")
		assert.Equal(t, fc.want, notesListIDs(filtered.Items), "--folder %s matches one folder exactly", fc.folder)
	}

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "idea", "notes", "list", "--folder", "missing", "--json")
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "unknown folder must fail; stdout=%s", stdout)
	assert.Contains(t, stderr, `"schema_version": "intents/v1alpha1"`)
	assert.Contains(t, stderr, `"code": "not_found"`)

	var viaList notesListPayload
	runCampJSONInDir(t, tc, campPath, &viaList, "idea", "list", "--status", "notes", "--sort", "created", "--json")
	assert.ElementsMatch(t, notesListIDs(all.Items), notesListIDs(viaList.Items), "idea list --status notes returns the notes list items")
	assert.Equal(t, notesListFolders(all.Items), notesListFolders(viaList.Items))

	var defaultList notesListPayload
	runCampJSONInDir(t, tc, campPath, &defaultList, "idea", "list", "--json")
	assert.Equal(t, []string{ideaAdd.ID}, notesListIDs(defaultList.Items), "default idea list stays inbox, ready, active")
	assert.Nil(t, defaultList.Items[0].Folder, "lifecycle items carry no folder")

	var mixed notesListPayload
	runCampJSONInDir(t, tc, campPath, &mixed, "idea", "list", "--status", "notes", "--status", "inbox", "--json")
	assert.ElementsMatch(t, []string{rootAdd.ID, paperAdd.ID, meetingID, ideaAdd.ID}, notesListIDs(mixed.Items))

	out, err = tc.RunCampInDir(campPath, "idea", "notes", "folders", "mv", "reading/papers", "reading/books")
	require.NoError(t, err, "folders mv: %s", out)
	var renamed notesListPayload
	runCampJSONInDir(t, tc, campPath, &renamed, "idea", "notes", "list", "--folder", "reading/books", "--json")
	require.Len(t, renamed.Items, 1)
	assert.Equal(t, paperAdd.ID, renamed.Items[0].ID)
	require.NotNil(t, renamed.Items[0].Folder)
	assert.Equal(t, "reading/books", *renamed.Items[0].Folder, "folder follows the file, not stale frontmatter")
	assert.True(t, strings.HasPrefix(renamed.Items[0].Path, ".campaign/intents/notes/reading/books/"), "path = %q", renamed.Items[0].Path)

	human, err := tc.RunCampInDir(campPath, "idea", "notes", "list")
	require.NoError(t, err, "notes list: %s", human)
	assert.Contains(t, human, "paper note")
	assert.Contains(t, human, "reading/books")
	assert.Contains(t, human, "3 note(s)")
	assert.False(t, slices.Contains(strings.Fields(human), "Shelved"), "archived notes stay out of the default human list:\n%s", human)
}
