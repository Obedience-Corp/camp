package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
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
