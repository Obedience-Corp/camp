package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func TestCampaignEntryMachineOmittedByDefault(t *testing.T) {
	c := campaignEntry{ID: "a1", Name: "camp", Type: "standard", Path: "/p", Org: "obey", Status: "active", Tags: []string{}}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "machine") {
		t.Errorf("machine key present without --remote (breaks byte-identical --json): %s", data)
	}
	c.Machine = "devbox"
	data, _ = json.Marshal(c)
	if !strings.Contains(string(data), `"machine":"devbox"`) {
		t.Errorf("machine key missing when set: %s", data)
	}
}

func TestOutputRemoteListSingleMachineIdenticalToLocal(t *testing.T) {
	// Only local rows, no remote machines or failures => no MACHINE column, output
	// byte-identical to the untouched local renderer.
	campaigns := []campaignEntry{
		{ID: "a1", Name: "c", Machine: "local", Type: "standard", Org: "obey", Status: "active", Tags: []string{}},
	}
	var remoteOut, plainOut bytes.Buffer
	if err := outputRemoteList(&remoteOut, io.Discard, campaigns, nil, "table"); err != nil {
		t.Fatal(err)
	}
	if err := outputCampaigns(&plainOut, campaigns, "table"); err != nil {
		t.Fatal(err)
	}
	if remoteOut.String() != plainOut.String() {
		t.Errorf("single-machine --remote differs from local render:\n%q\nvs\n%q", remoteOut.String(), plainOut.String())
	}
	if strings.Contains(remoteOut.String(), "MACHINE") {
		t.Errorf("single-machine output must not have a MACHINE column: %q", remoteOut.String())
	}
}

func TestOutputRemoteListMultiMachineAddsColumnAndUnreachableRow(t *testing.T) {
	campaigns := []campaignEntry{
		{ID: "a1", Name: "local-camp", Machine: "local", Type: "standard", Org: "obey", Status: "active", Tags: []string{}},
		{ID: "b2", Name: "remote-camp", Machine: "devbox", Type: "standard", Org: "obey", Status: "active", Tags: []string{}},
	}
	results := []remoteResult{
		{machineID: "devbox", rows: campaigns[1:2]},
		{machineID: "dead", err: errors.New("dial timeout")},
	}
	var out, errBuf bytes.Buffer
	if err := outputRemoteList(&out, &errBuf, campaigns, results, "table"); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "MACHINE") {
		t.Error("multi-machine output missing MACHINE column")
	}
	if !strings.Contains(s, "devbox") || !strings.Contains(s, "remote-camp") {
		t.Errorf("missing remote machine row: %q", s)
	}
	if !strings.Contains(s, "dead") || !strings.Contains(s, "unreachable") {
		t.Errorf("missing unreachable muted row: %q", s)
	}
	campLine := lineContaining(s, "remote-camp")
	failLine := lineContaining(s, "dead")
	if campLine == "" || failLine == "" {
		t.Fatalf("missing campaign or failure line: %q", s)
	}
	if strings.Contains(campLine, "dead") || strings.Contains(campLine, "unreachable") {
		t.Errorf("failed machine rendered in the campaign table row: %q", campLine)
	}
	if strings.Index(s, failLine) < strings.Index(s, campLine) {
		t.Errorf("failed machine must render below the table:\n%s", s)
	}
}

// A camp-not-found / unreachable message used to sit in the ID column of the
// same tabwriter as successful rows, so every ID cell padded to that length.
// Failed machines belong under the table; campaign rows must not change.
func TestRenderRemoteTableFailedMachinesDoNotPadIDColumn(t *testing.T) {
	campaigns := []campaignEntry{
		{ID: "a1", Name: "local-camp", Machine: "local", Type: "standard", Org: "obey", Status: "active", Path: "/p", Tags: []string{}},
		{ID: "b2", Name: "remote-camp", Machine: "devbox", Type: "standard", Org: "obey", Status: "active", Path: "/q", Tags: []string{}},
	}
	long := errors.New("remote camp not found on archdtop: nothing named camp on the account's login-shell PATH, and none of camp's usual install locations (~/go/bin) has one; if it lives elsewhere, set CAMP_REMOTE_CAMP_PATH to its exact path on that machine")
	var clean, dirty bytes.Buffer
	if err := outputRemoteList(&clean, io.Discard, campaigns, []remoteResult{{machineID: "devbox"}}, "table"); err != nil {
		t.Fatal(err)
	}
	if err := outputRemoteList(&dirty, io.Discard, campaigns, []remoteResult{
		{machineID: "devbox", rows: campaigns[1:2]},
		{machineID: "archdtop", err: long},
	}, "table"); err != nil {
		t.Fatal(err)
	}
	cleanRow := lineContaining(clean.String(), "local-camp")
	dirtyRow := lineContaining(dirty.String(), "local-camp")
	if cleanRow == "" || dirtyRow == "" {
		t.Fatalf("missing local-camp row\nclean=%q\ndirty=%q", clean.String(), dirty.String())
	}
	if cleanRow != dirtyRow {
		t.Errorf("campaign row padded by failed-machine error:\nclean=%q\ndirty=%q", cleanRow, dirtyRow)
	}
	fail := lineContaining(dirty.String(), "archdtop")
	if fail == "" || !strings.Contains(fail, "CAMP_REMOTE_CAMP_PATH") {
		t.Fatalf("failed machine not rendered below the table:\n%s", dirty.String())
	}
	if strings.Contains(dirtyRow, "archdtop") || strings.Contains(dirtyRow, "CAMP_REMOTE_CAMP_PATH") {
		t.Errorf("failure text leaked into a campaign row: %q", dirtyRow)
	}
	if strings.Index(dirty.String(), fail) <= strings.Index(dirty.String(), dirtyRow) {
		t.Errorf("failed machine must render below the table:\n%s", dirty.String())
	}
}

func lineContaining(s, sub string) string {
	for _, ln := range strings.Split(s, "\n") {
		if strings.Contains(ln, sub) {
			return ln
		}
	}
	return ""
}

func TestFormatUnreachableErrPrefersHopClassification(t *testing.T) {
	stderr := "# Tailscale SSH requires an additional check.\n# To authenticate, visit: https://login.tailscale.com/a/xyz\n"
	// Simulate a classified timeout wrap the way remote.Run produces.
	err := errors.New("Tailscale SSH requires a one-time browser check — open https://login.tailscale.com/a/xyz, approve, then retry (camp cannot complete this interactively) (while connecting to host): context deadline exceeded")
	// HopFailureDetail needs ParseTailscaleCheckURL markers in the text.
	_ = stderr
	got := formatUnreachableErr(err)
	if !strings.Contains(got, "login.tailscale.com/a/xyz") {
		t.Errorf("formatUnreachableErr = %q, want check URL", got)
	}
}

func TestOutputRemoteListJSONCleanStdoutUnreachableToStderr(t *testing.T) {
	campaigns := []campaignEntry{
		{ID: "a1", Name: "c", Machine: "local", Type: "standard", Org: "obey", Status: "active", Tags: []string{}},
	}
	results := []remoteResult{{machineID: "dead", err: errors.New("dial timeout")}}
	var out, errBuf bytes.Buffer
	if err := outputRemoteList(&out, &errBuf, campaigns, results, "json"); err != nil {
		t.Fatal(err)
	}
	// stdout must be valid JSON (no unreachable warning polluting it).
	var rows []campaignEntry
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("stdout is not clean JSON: %v\n%s", err, out.String())
	}
	if !strings.Contains(errBuf.String(), "dead") || !strings.Contains(errBuf.String(), "unreachable") {
		t.Errorf("unreachable machine not reported on stderr: %q", errBuf.String())
	}
}

// A machine whose far side found no camp is labeled as such in the table row
// and the stderr warning; every other failure keeps the "unreachable" label.
func TestRemoteFailureLabelDistinguishesCampNotFound(t *testing.T) {
	notFound := camperrors.Wrap(camperrors.NewCommand("ssh devbox", 127, "camp: not found", nil), "remote camp not found on devbox")
	if got := remoteFailureLabel(notFound); got != "camp not found" {
		t.Errorf("exit 127 labeled %q, want %q", got, "camp not found")
	}
	if got := remoteFailureLabel(errors.New("ssh: connect to host devbox: timed out")); got != "unreachable" {
		t.Errorf("timeout labeled %q, want %q", got, "unreachable")
	}

	var table, warn bytes.Buffer
	results := []remoteResult{{machineID: "devbox", err: notFound}}
	if err := outputRemoteList(&table, io.Discard, nil, results, "table"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(table.String(), "(camp not found:") || strings.Contains(table.String(), "unreachable") {
		t.Errorf("table row for exit 127 = %q", table.String())
	}
	if strings.Contains(table.String(), "MACHINE") {
		t.Errorf("failure-only output must not print an empty table: %q", table.String())
	}
	warnUnreachable(&warn, results)
	if !strings.Contains(warn.String(), "devbox camp not found:") {
		t.Errorf("stderr warning for exit 127 = %q", warn.String())
	}
}
