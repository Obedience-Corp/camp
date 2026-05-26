//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type doctorFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Target   string `json:"target"`
	Message  string `json:"message"`
}

type doctorReport struct {
	SchemaVersion string          `json:"schema_version"`
	Findings      []doctorFinding `json:"findings"`
}

const intentFrontmatter = `---
id: int_%02d
status: %s
title: idea %02d
---

body
`

const directoryWorkitemMarker = `version: v1alpha5
kind: workitem
id: %s
type: %s
title: %s
`

const festivalGoal = `# Festival Goal

Placeholder.
`

func seedDoctorIntentFestivalFixture(t *testing.T, tc *TestContainer, dir string) {
	t.Helper()
	initWorkflowCampaign(t, tc, dir)

	for status, count := range map[string]int{"inbox": 20, "active": 15, "ready": 10} {
		for i := 1; i <= count; i++ {
			body := fmt.Sprintf(intentFrontmatter, i, status, i)
			require.NoError(t, tc.WriteFile(
				fmt.Sprintf("%s/.campaign/intents/%s/%02d-idea.md", dir, status, i), body))
		}
	}

	festivals := []string{
		"festivals/planning/01-test-CW0001",
		"festivals/planning/02-test-CW0002",
		"festivals/active/03-test-CW0003",
		"festivals/dungeon/completed/04-test-CW0004",
	}
	for _, p := range festivals {
		require.NoError(t, tc.WriteFile(dir+"/"+p+"/FESTIVAL_GOAL.md", festivalGoal))
	}
}

func seedDoctorDirectoryWorkitemFixture(t *testing.T, tc *TestContainer, dir string) []string {
	t.Helper()
	initWorkflowCampaign(t, tc, dir)

	paths := []struct {
		rel  string
		kind string
		id   string
	}{
		{"workflow/design/under-design", "design", "design-under-design-2026-05-25"},
		{"workflow/explore/spike-foo", "explore", "explore-spike-foo-2026-05-25"},
		{"workflow/research/topic-bar", "research", "research-topic-bar-2026-05-25"},
	}

	relMarkers := make([]string, 0, len(paths))
	for _, p := range paths {
		marker := fmt.Sprintf(directoryWorkitemMarker, p.id, p.kind, p.id)
		full := fmt.Sprintf("%s/%s/.workitem", dir, p.rel)
		require.NoError(t, tc.WriteFile(full, marker))
		relMarkers = append(relMarkers, p.rel+"/.workitem")
	}
	return relMarkers
}

func parseDoctorReport(t *testing.T, raw string) doctorReport {
	t.Helper()
	var r doctorReport
	err := json.Unmarshal([]byte(raw), &r)
	require.NoError(t, err, "doctor --json must produce valid JSON, got: %s", raw)
	return r
}

func countByCode(report doctorReport, code string) int {
	n := 0
	for _, f := range report.Findings {
		if f.Code == code {
			n++
		}
	}
	return n
}

func TestIntegration_Doctor_NoFalsePositiveOnIntents(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/doctor-intents-no-fp"
	seedDoctorIntentFestivalFixture(t, tc, dir)

	out, err := tc.RunCampInDir(dir, "workitem", "doctor", "--json")
	require.NoError(t, err, "doctor --json: %s", out)
	report := parseDoctorReport(t, out)
	missing := countByCode(report, "workitem.ref.missing")
	assert.Equal(t, 0, missing,
		"expected 0 workitem.ref.missing findings on intent+festival fixture, got %d:\n%s",
		missing, out)
}

func TestIntegration_Doctor_FixIsNoOpOnIntents(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/doctor-intents-fix-noop"
	seedDoctorIntentFestivalFixture(t, tc, dir)

	out, err := tc.RunCampInDir(dir, "workitem", "doctor", "--fix", "--json")
	require.NoError(t, err, "doctor --fix --json: %s", out)
	report := parseDoctorReport(t, out)
	missing := countByCode(report, "workitem.ref.missing")
	assert.Equal(t, 0, missing,
		"doctor --fix on intent+festival fixture must not produce ref.missing findings, got:\n%s", out)
}

func TestIntegration_Doctor_FixBackfillsDirectoryWorkitems(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/doctor-fix-backfill"
	markers := seedDoctorDirectoryWorkitemFixture(t, tc, dir)

	before, err := tc.RunCampInDir(dir, "workitem", "doctor", "--json")
	require.NoError(t, err, "doctor --json: %s", before)
	beforeReport := parseDoctorReport(t, before)
	beforeMissing := countByCode(beforeReport, "workitem.ref.missing")
	assert.Equal(t, 3, beforeMissing,
		"expected 3 ref.missing findings before --fix, got %d:\n%s", beforeMissing, before)

	fixOut, err := tc.RunCampInDir(dir, "workitem", "doctor", "--fix")
	require.NoError(t, err, "doctor --fix: %s", fixOut)

	after, err := tc.RunCampInDir(dir, "workitem", "doctor", "--json")
	require.NoError(t, err, "doctor --json after fix: %s", after)
	afterReport := parseDoctorReport(t, after)
	afterMissing := countByCode(afterReport, "workitem.ref.missing")
	assert.Equal(t, 0, afterMissing,
		"expected 0 ref.missing findings after --fix, got %d:\n%s", afterMissing, after)

	for _, rel := range markers {
		body, err := tc.ReadFile(dir + "/" + rel)
		require.NoError(t, err)
		assert.Contains(t, body, "ref: WI-",
			"expected backfilled ref in %s, got:\n%s", rel, body)
	}
}
