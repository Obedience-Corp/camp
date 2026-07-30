//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func residentDoctorFindings(t *testing.T, tc *TestContainer, path string) []doctorFinding {
	t.Helper()
	// doctor exits non-zero when it reports findings, so ignore the exit code.
	stdout, _, _, err := tc.RunCampSplitInDir(path, "workitem", "doctor", "--json")
	require.NoError(t, err)
	return doctorFindings(t, stdout)
}

func findingFor(findings []doctorFinding, code, target string) (doctorFinding, bool) {
	for _, f := range findings {
		if f.Code == code && f.Target == target {
			return f, true
		}
	}
	return doctorFinding{}, false
}

func TestDoctorResidents_ReportsUnstampedAndHomeless(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/doctor-residents"
	_, err := tc.InitCampaign(path, "doctor-residents", "product")
	require.NoError(t, err)

	// Neither a resident nor a festival.
	require.NoError(t, tc.WriteFile(path+"/festivals/active/mystery/NOTES.md", "# ?\n"))
	// A real festival on the same stage: must not be flagged.
	require.NoError(t, tc.WriteFile(path+"/festivals/active/realfest/fest.yaml",
		"version: \"1.0\"\nmetadata:\n  id: RF\n  name: Real\n"))
	// A stamped resident whose type root does not exist.
	require.NoError(t, tc.WriteFile(path+"/festivals/active/orphan/.workitem",
		"version: v1alpha8\nkind: workitem\nid: bug-orphan-1\ntype: bug\ntitle: Orphan\nref: WI-aaaaaa\n"))
	require.NoError(t, tc.WriteFile(path+"/festivals/active/orphan/README.md", "# Orphan\n"))
	// A stamped resident whose type root does exist: must not be flagged.
	require.NoError(t, tc.WriteFile(path+"/festivals/ready/good/.workitem",
		"version: v1alpha8\nkind: workitem\nid: design-good-1\ntype: design\ntitle: Good\nref: WI-bbbbbb\n"))
	require.NoError(t, tc.WriteFile(path+"/festivals/ready/good/README.md", "# Good\n"))
	_, _, err = tc.ExecCommand("sh", "-c", "mkdir -p "+path+"/workflow/design")
	require.NoError(t, err)

	findings := residentDoctorFindings(t, tc, path)

	unstamped, ok := findingFor(findings, "workitem.resident.unstamped", "festivals/active/mystery")
	require.True(t, ok, "missing unstamped finding: %+v", findings)
	assert.Equal(t, "warning", unstamped.Severity)
	assert.False(t, unstamped.AutoFixable, "camp cannot guess a type; --fix must not touch it")
	assert.NotEmpty(t, unstamped.FixHint)

	homeless, ok := findingFor(findings, "workitem.resident.missing-home", "festivals/active/orphan")
	require.True(t, ok, "missing homeless finding: %+v", findings)
	assert.Equal(t, "warning", homeless.Severity)
	assert.False(t, homeless.AutoFixable, "camp must not invent a type root")
	assert.Contains(t, homeless.Message, "workflow/bug/")

	_, ok = findingFor(findings, "workitem.resident.unstamped", "festivals/active/realfest")
	assert.False(t, ok, "a directory with fest.yaml is a festival, not an unstamped resident")
	_, ok = findingFor(findings, "workitem.resident.unstamped", "festivals/ready/good")
	assert.False(t, ok, "a stamped resident is not unstamped")
	_, ok = findingFor(findings, "workitem.resident.missing-home", "festivals/ready/good")
	assert.False(t, ok, "workflow/design exists, so this resident has a home")
}

// --fix must leave both findings in place: they are the two cases camp refuses to
// guess at.
func TestDoctorResidents_FixDoesNotResolveThem(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/doctor-residents-fix"
	_, err := tc.InitCampaign(path, "doctor-residents-fix", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(path+"/festivals/active/mystery/NOTES.md", "# ?\n"))
	require.NoError(t, tc.WriteFile(path+"/festivals/active/orphan/.workitem",
		"version: v1alpha8\nkind: workitem\nid: bug-orphan-1\ntype: bug\ntitle: Orphan\nref: WI-aaaaaa\n"))
	require.NoError(t, tc.WriteFile(path+"/festivals/active/orphan/README.md", "# Orphan\n"))

	_, _, _, err = tc.RunCampSplitInDir(path, "workitem", "doctor", "--fix")
	require.NoError(t, err)

	findings := residentDoctorFindings(t, tc, path)
	_, ok := findingFor(findings, "workitem.resident.unstamped", "festivals/active/mystery")
	assert.True(t, ok, "--fix must not silently resolve an unstamped directory")
	_, ok = findingFor(findings, "workitem.resident.missing-home", "festivals/active/orphan")
	assert.True(t, ok, "--fix must not invent workflow/bug/")

	exists, err := tc.CheckFileExists(path + "/festivals/active/mystery/.workitem")
	require.NoError(t, err)
	assert.False(t, exists, "--fix must not stamp a directory it cannot classify")
	exists, err = tc.CheckDirExists(path + "/workflow/bug")
	require.NoError(t, err)
	assert.False(t, exists, "--fix must not create a type root")
}
