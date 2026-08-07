//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTriageDriver_BriefIsGeneratedForTheRun covers the rendering.
func TestTriageDriver_BriefIsGeneratedForTheRun(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-driver-render", 3, 0)
	runID, runDir := startTriageRun(t, tc, path)

	brief, err := tc.ReadFile(runDir + "/DRIVER.md")
	require.NoError(t, err)

	// Every command names this run, so an agent never substitutes a
	// placeholder — which is the substitution it would eventually get wrong.
	assert.Contains(t, brief, "camp triage queue --run "+runID)
	assert.Contains(t, brief, "camp triage evidence template <stable-id> --run "+runID)
	assert.NotContains(t, brief, "evidence template <stable-id> --run "+runID+" --json",
		"the brief must not name a flag the command does not have")
	assert.Contains(t, brief, "camp triage evidence set <stable-id> --run "+runID)
	assert.Contains(t, brief, "camp triage propose <stable-id> --run "+runID)

	// The schema with a filled example, not a bare type listing.
	assert.Contains(t, brief, `"stale_assumptions"`)
	assert.Contains(t, brief, `"anchors"`)
	assert.Contains(t, brief, "Anchors are the part that expires")

	// The routing tiers as prose, from the profile.
	assert.Contains(t, brief, "model or pass for per-item document reads")
	assert.Contains(t, brief, "workers concurrently")

	// The advisory rules, stated as rules.
	for _, rule := range []string{
		"Read-only", "No lifecycle mutations", "Do not edit workitems",
		"Submit only through", "always require a human's recorded approval",
		"Rationale is required",
	} {
		assert.Contains(t, brief, rule, "the brief must state %q", rule)
	}

	// The completion check.
	assert.Contains(t, brief, "An empty queue means every row is judged")
}

// TestTriageDriver_BriefIsExecutableAsWritten is the acceptance for doc 08
// driver 1, without invoking any model: a shell fake reads the commands out of
// DRIVER.md, runs them, and the run reaches reviewing.
//
// It validates the contract, not intelligence. If the brief's commands are
// wrong — a stale flag, a renamed subcommand — this fails, which is the whole
// point of generating the document rather than shipping prose about it.
func TestTriageDriver_BriefIsExecutableAsWritten(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-driver-scripted", 3, 0)
	runID, runDir := startTriageRun(t, tc, path)

	brief, err := tc.ReadFile(runDir + "/DRIVER.md")
	require.NoError(t, err)

	// Take the arguments straight out of the brief rather than retyping them:
	// retyping would test my memory of the commands, not the document. They
	// run through the harness because `camp` is not on the container's PATH,
	// but the argv is the brief's own.
	queueArgs := argsFromBrief(t, brief, "camp triage queue --run ")
	out, err := tc.RunCampInDir(path, queueArgs...)
	require.NoError(t, err, out)
	assert.Contains(t, out, "design-item-1")

	// Drive the loop the brief describes for every row.
	for _, id := range manifestRowIDs(t, tc, runDir) {
		templateArgs := substituteID(
			argsFromBrief(t, brief, "camp triage evidence template <stable-id> --run "), id)
		out, err = tc.RunCampInDir(path, templateArgs...)
		require.NoError(t, err, "the template command from the brief must run: %s", out)

		// The zero-judgment path the brief documents, verbatim.
		setArgs := substituteID(
			argsFromBrief(t, brief, "camp triage evidence set <stable-id> --run "), id)
		setArgs = replaceArg(setArgs, "--file", "--no-evidence")
		out, err = tc.RunCampInDir(path, setArgs...)
		require.NoError(t, err, out)

		proposeOut, proposeErr := tc.RunCampInDir(path, "triage", "propose", id,
			"--run", runID, "--disposition", "parked", "--summary", "scripted driver")
		require.NoError(t, proposeErr, proposeOut)
	}

	// The completion check from the brief: an empty queue.
	out, err = tc.RunCampInDir(path, queueArgs...)
	require.NoError(t, err, out)
	assert.NotContains(t, out, "needs evidence",
		"every row is judged after following the brief")

	// And the run can now enter review, which is what "finished" means.
	approveOut, err := tc.RunCampInDir(path, "triage", "approve", "--batch", "1", "--json")
	require.NoError(t, err, approveOut)

	statusOut, err := tc.RunCampInDir(path, "triage", "status")
	require.NoError(t, err, statusOut)
	assert.Contains(t, statusOut, "reviewing",
		"following the brief takes the run to reviewing")
}

// argsFromBrief returns the argv of the first command line in the brief that
// starts with prefix, minus the leading "camp", so the test executes the
// document rather than a copy of it.
func argsFromBrief(t *testing.T, brief, prefix string) []string {
	t.Helper()
	for _, line := range strings.Split(brief, "\n") {
		if strings.HasPrefix(line, prefix) {
			fields := strings.Fields(strings.TrimSpace(line))
			require.Equal(t, "camp", fields[0])
			return fields[1:]
		}
	}
	t.Fatalf("no command starting %q in DRIVER.md", prefix)
	return nil
}

// substituteID replaces the brief's placeholder with a real row.
func substituteID(args []string, id string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = strings.ReplaceAll(arg, "<stable-id>", id)
	}
	return out
}

// replaceArg swaps a flag and its value for a single replacement flag.
func replaceArg(args []string, flag, replacement string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == flag {
			i++ // skip the flag's value
			out = append(out, replacement)
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// TestTriageDriver_EvidenceTemplatePreFillsCampHeldFacts covers doc 08's
// driver 2: the deterministic fields come from camp, so a human's unassisted
// path starts from facts rather than a blank record.
func TestTriageDriver_EvidenceTemplatePreFillsCampHeldFacts(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupTriageCampaign(t, tc, "triage-driver-template", 2, 0)
	startTriageRun(t, tc, path)

	output, err := tc.RunCampInDir(path, "triage", "evidence", "template",
		"design-item-1")
	require.NoError(t, err, output)

	body := extractJSON(t, output)
	assert.Contains(t, body, "design-item-1")
	assert.Contains(t, body, "signals",
		"camp-measured facts are kept separate from judgment fields")

	// The judgment fields are present but empty: the template makes the trail,
	// the reader supplies the reading.
	for _, field := range []string{"original_goal", "delivered", "confidence"} {
		assert.Contains(t, body, field)
	}
}
