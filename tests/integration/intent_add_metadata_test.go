//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type intentMetadataItem struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Type              string   `json:"type"`
	Status            string   `json:"status"`
	Concept           string   `json:"concept"`
	Author            string   `json:"author"`
	Priority          string   `json:"priority"`
	Horizon           string   `json:"horizon"`
	Tags              []string `json:"tags"`
	BlockedBy         []string `json:"blocked_by"`
	DependsOn         []string `json:"depends_on"`
	PromotionCriteria string   `json:"promotion_criteria"`
	Path              string   `json:"path"`
}

func showIntentMetadata(t *testing.T, tc *TestContainer, camp, id string) intentMetadataItem {
	t.Helper()

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(camp, "intent", "show", id, "--json")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "intent show --json failed: %s", stderr)

	var payload struct {
		Items []intentMetadataItem `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload), "show output: %s", stdout)
	require.Len(t, payload.Items, 1)
	return payload.Items[0]
}

func commitCount(t *testing.T, tc *TestContainer, camp string) string {
	t.Helper()
	return tc.GitOutput(t, camp, "rev-list", "--count", "HEAD")
}

func inboxFiles(t *testing.T, tc *TestContainer, camp string) []string {
	t.Helper()
	out, _, err := tc.ExecCommand("sh", "-c", "ls "+camp+"/.campaign/intents/inbox 2>/dev/null | grep '\\.md$' || true")
	require.NoError(t, err)
	return strings.Fields(out)
}

// TestIntentAdd_OneCallCapturesCompleteIntent runs the documented one-call
// form without a terminal: title, stdin body and every metadata flag in a
// single command, then reads each value back through `intent show --json`.
func TestIntentAdd_OneCallCapturesCompleteIntent(t *testing.T) {
	tc := GetSharedContainer(t)

	camp := "/campaigns/intent-one-call"
	_, err := tc.InitCampaign(camp, "intent-one-call", "product")
	require.NoError(t, err)
	before := commitCount(t, tc, camp)

	script := `cd ` + camp + ` && /camp intent add "Login returns 500 after password reset" \
  --type bug --concept projects/camp --priority high --horizon next \
  --tag auth --tag login \
  --blocked-by "reset-flow-20260101-000001" --depends-on "AC0002" --depends-on "odd: \"ref\" #1" \
  --promotion-criteria "Decide: \"ship\" or # wait?" \
  --json --body-file - 2>/tmp/one_call_stderr <<'EOF'
Logging in right after a password reset returns a 500.

Found while testing the reset flow.

Done when: the first login after a reset succeeds.
EOF`
	out, exitCode, err := tc.ExecCommand("sh", "-c", script)
	require.NoError(t, err)
	stderr, _, _ := tc.ExecCommand("cat", "/tmp/one_call_stderr")
	require.Equal(t, 0, exitCode, "one-call add failed: %s\nstderr: %s", out, stderr)

	var created struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &created), "add --json output: %s", out)
	require.NotEmpty(t, created.ID)

	item := showIntentMetadata(t, tc, camp, created.ID)
	assert.Equal(t, "Login returns 500 after password reset", item.Title)
	assert.Equal(t, "bug", item.Type)
	assert.Equal(t, "inbox", item.Status)
	assert.Equal(t, "projects/camp", item.Concept)
	assert.Equal(t, "agent", item.Author)
	assert.Equal(t, "high", item.Priority)
	assert.Equal(t, "next", item.Horizon)
	assert.Equal(t, []string{"auth", "login"}, item.Tags)
	assert.Equal(t, []string{"reset-flow-20260101-000001"}, item.BlockedBy)
	assert.Equal(t, []string{"AC0002", `odd: "ref" #1`}, item.DependsOn)
	assert.Equal(t, `Decide: "ship" or # wait?`, item.PromotionCriteria)

	content, err := tc.ReadFile(camp + "/" + created.Path)
	require.NoError(t, err)
	assert.Contains(t, content, "## Description\n\nLogging in right after a password reset returns a 500.\n\nFound while testing the reset flow.\n\nDone when: the first login after a reset succeeds.\n")

	assert.Len(t, inboxFiles(t, tc, camp), 1)

	after := commitCount(t, tc, camp)
	assert.NotEqual(t, before, after, "capture should commit")
	committed := tc.GitOutput(t, camp, "show", "--name-only", "--format=", "HEAD")
	assert.ElementsMatch(t,
		[]string{created.Path, ".campaign/intents/.intents.jsonl"},
		strings.Fields(committed),
		"one capture should make one commit holding the intent and the audit log")
	parent := tc.GitOutput(t, camp, "rev-list", "--count", "HEAD~1")
	assert.Equal(t, before, parent, "capture should add exactly one commit")
	assert.Empty(t, tc.GitOutput(t, camp, "status", "--porcelain", "--", ".campaign/intents"))
}

// TestIntentAdd_DefaultsUnchangedWithoutMetadataFlags pins the frontmatter a
// title-only capture writes, so the new flags cannot change the default file.
func TestIntentAdd_DefaultsUnchangedWithoutMetadataFlags(t *testing.T) {
	tc := GetSharedContainer(t)

	camp := "/campaigns/intent-defaults"
	_, err := tc.InitCampaign(camp, "intent-defaults", "product")
	require.NoError(t, err)

	_, err = tc.RunCampInDir(camp, "intent", "add", "Title only", "--no-commit")
	require.NoError(t, err)

	files := inboxFiles(t, tc, camp)
	require.Len(t, files, 1)
	content, err := tc.ReadFile(camp + "/.campaign/intents/inbox/" + files[0])
	require.NoError(t, err)
	assert.Contains(t, content, "tags: []\n\npriority: medium\nhorizon: later\n\nblocked_by: []\ndepends_on: []\n\npromotion_criteria: >\n  What needs to be true before this can become a Festival?\n---\n")
}

// TestIntentAdd_InvalidMetadataWritesNothing covers the rejected-flag cases:
// each must fail with a message naming the flag and leave no file or commit.
func TestIntentAdd_InvalidMetadataWritesNothing(t *testing.T) {
	tc := GetSharedContainer(t)

	camp := "/campaigns/intent-invalid-metadata"
	_, err := tc.InitCampaign(camp, "intent-invalid-metadata", "product")
	require.NoError(t, err)
	before := commitCount(t, tc, camp)

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "priority", args: []string{"Bad priority", "--priority", "urgent"}, want: []string{"--priority", "low, medium, high"}},
		{name: "horizon", args: []string{"Bad horizon", "--horizon", "soon"}, want: []string{"--horizon", "now, next, later, someday"}},
		{name: "type", args: []string{"Bad type", "--type", "bogus"}, want: []string{"--type", "bogus"}},
		{name: "empty blocked-by", args: []string{"Bad dependency", "--blocked-by", ""}, want: []string{"--blocked-by", "must not be empty"}},
		{name: "note with priority", args: []string{"A note", "--note", "--priority", "high"}, want: []string{"--note cannot be combined with --priority"}},
		{name: "note with depends-on", args: []string{"A note", "--note", "--depends-on", "x"}, want: []string{"--note cannot be combined with --depends-on"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := tc.RunCampInDir(camp, append([]string{"intent", "add"}, tt.args...)...)
			require.Error(t, err, "expected a usage error, got: %s", out)
			for _, want := range tt.want {
				assert.Contains(t, out, want)
			}
		})

		t.Run(tt.name+" json", func(t *testing.T) {
			args := append([]string{"intent", "add", "--json"}, tt.args...)
			stdout, stderr, exitCode, err := tc.RunCampSplitInDir(camp, args...)
			require.NoError(t, err)
			require.NotEqual(t, 0, exitCode)
			assert.Empty(t, strings.TrimSpace(stdout), "a rejected flag must not print a success payload")

			var envelope struct {
				SchemaVersion string         `json:"schema_version"`
				Error         map[string]any `json:"error"`
			}
			require.NoError(t, json.Unmarshal([]byte(stderr), &envelope), "stderr: %s", stderr)
			assert.NotEmpty(t, envelope.SchemaVersion)
			assert.NotEmpty(t, envelope.Error)
		})
	}

	assert.Empty(t, inboxFiles(t, tc, camp), "rejected captures must not write an intent")
	notes, _, err := tc.ExecCommand("sh", "-c", "ls "+camp+"/.campaign/intents/notes 2>/dev/null | grep '\\.md$' || true")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(notes), "rejected captures must not write a note")
	assert.Equal(t, before, commitCount(t, tc, camp), "rejected captures must not commit")
}

// TestIntentAdd_NoTitleWithoutTerminalNamesOneCallForm checks the error an
// agent sees when it omits the title.
func TestIntentAdd_NoTitleWithoutTerminalNamesOneCallForm(t *testing.T) {
	tc := GetSharedContainer(t)

	camp := "/campaigns/intent-no-title"
	_, err := tc.InitCampaign(camp, "intent-no-title", "product")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(camp, "intent", "add", "--body", "a body without a title")
	require.Error(t, err)
	assert.Contains(t, out, "title argument required in non-interactive mode")
	assert.Contains(t, out, `camp idea add "<title>" --body-file -`)
	assert.Empty(t, inboxFiles(t, tc, camp))
}

// TestIntentEdit_SetsDependenciesAndCriteria covers the edit flags that mirror
// the new add flags, including clearing a list and the audit trail.
func TestIntentEdit_SetsDependenciesAndCriteria(t *testing.T) {
	tc := GetSharedContainer(t)

	camp := "/campaigns/intent-edit-metadata"
	_, err := tc.InitCampaign(camp, "intent-edit-metadata", "product")
	require.NoError(t, err)

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(camp, "intent", "add", "Edit target", "--json")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, stderr)
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &created))

	_, err = tc.RunCampInDir(camp, "intent", "edit", created.ID,
		"--blocked-by", "first-20260101-000001", "--blocked-by", "AC0002",
		"--depends-on", "second-20260101-000002",
		"--promotion-criteria", "The contract is agreed")
	require.NoError(t, err)

	item := showIntentMetadata(t, tc, camp, created.ID)
	assert.Equal(t, []string{"first-20260101-000001", "AC0002"}, item.BlockedBy)
	assert.Equal(t, []string{"second-20260101-000002"}, item.DependsOn)
	assert.Equal(t, "The contract is agreed", item.PromotionCriteria)

	audit, err := tc.ReadFile(camp + "/.campaign/intents/.intents.jsonl")
	require.NoError(t, err)
	for _, field := range []string{`"field":"blocked_by"`, `"field":"depends_on"`, `"field":"promotion_criteria"`} {
		assert.Contains(t, audit, field)
	}

	_, err = tc.RunCampInDir(camp, "intent", "edit", created.ID, "--blocked-by", "")
	require.NoError(t, err)
	item = showIntentMetadata(t, tc, camp, created.ID)
	assert.Empty(t, item.BlockedBy, `--blocked-by "" should clear the list`)
	assert.Equal(t, []string{"second-20260101-000002"}, item.DependsOn, "clearing one list must not touch the other")

	out, err := tc.RunCampInDir(camp, "intent", "edit", created.ID, "--depends-on", "ok", "--depends-on", "")
	require.Error(t, err, "an empty reference among others is a usage error: %s", out)
	assert.Contains(t, out, "must not be empty")
	assert.Empty(t, tc.GitOutput(t, camp, "status", "--porcelain", "--", ".campaign/intents"))
}

// TestInit_ScaffoldTeachesOneCallCapture checks that a new camp's agent-facing
// text shows the one-call form rather than a title-only capture.
func TestInit_ScaffoldTeachesOneCallCapture(t *testing.T) {
	tc := GetSharedContainer(t)

	camp := "/campaigns/intent-scaffold-text"
	_, err := tc.InitCampaign(camp, "intent-scaffold-text", "product")
	require.NoError(t, err)

	workflows, err := tc.ReadFile(camp + "/.campaign/skills/campaign-workflows/SKILL.md")
	require.NoError(t, err)
	for _, want := range []string{"--body-file - <<'EOF'", "`--priority`", "`--horizon`", "`--blocked-by`, `--depends-on`", "`--promotion-criteria`"} {
		assert.Contains(t, workflows, want)
	}

	structure, err := tc.ReadFile(camp + "/.campaign/skills/campaign-structure/SKILL.md")
	require.NoError(t, err)
	assert.Contains(t, structure, `camp intent add "<title>" --body-file -`)

	guide, err := tc.ReadFile(camp + "/.campaign/intents/OBEY.md")
	require.NoError(t, err)
	assert.Contains(t, guide, "--body-file - <<'EOF'")
}
