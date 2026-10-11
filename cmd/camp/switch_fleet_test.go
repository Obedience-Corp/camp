package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
)

func TestNormalizeCampName(t *testing.T) {
	if !sameCampaignName("mytools", "My_Tools") {
		t.Fatal("mytools and My_Tools are the same camp")
	}
	if !sameCampaignName("My-Tools", "my tools") {
		t.Fatal("separators are ignored")
	}
	if sameCampaignName("sarah", "My_Tools") {
		t.Fatal("sarah is not My_Tools")
	}
	if sameCampaignName("", "My_Tools") {
		t.Fatal("empty name must not match")
	}
}

func TestMatchFleetCampsNormalizedAndAmbiguous(t *testing.T) {
	camps := []fleetCamp{
		{Machine: "mac-studio", Name: "My_Tools"},
		{Machine: "mac-studio", Name: "sarah"},
		{Machine: "other", Name: "notes"},
	}
	hit, err := matchFleetCamps("mytools", camps)
	if err != nil {
		t.Fatal(err)
	}
	if hit.Machine != "mac-studio" || hit.Name != "My_Tools" {
		t.Fatalf("hit = %+v", hit)
	}

	_, err = matchFleetCamps("nope", camps)
	if !errors.Is(err, errFleetMiss) {
		t.Fatalf("missing camp err = %v", err)
	}

	dupes := append(camps, fleetCamp{Machine: "other", Name: "My_Tools"})
	_, err = matchFleetCamps("mytools", dupes)
	if err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Fatalf("duplicate name err = %v", err)
	}
}

func TestPrepareResumeSSHInjectsSocket(t *testing.T) {
	var buf strings.Builder
	m := &machines.Machine{ID: "devbox", Host: "devbox.ts.net", SSHUser: "lance", AuthMethod: machines.AuthSSHAgent}
	if err := emitShellConnect(&buf, true, "/srv/campaigns/obey", remote.Direct(m), "v1;host=example"); err != nil {
		t.Fatal(err)
	}
	hop, err := splitHopLine(buf.String())
	if err != nil {
		t.Fatal(err)
	}
	forward := hopResumeForward{
		localSock: "/tmp/camp-hr-l.sock", remoteSock: "/tmp/camp-hr-r.sock",
		loopLocalSock: "/tmp/camp-hr-t.sock", loopAddr: "127.0.0.1:50123",
		token: "c0ffee", tokenFile: "/tmp/camp-ht-Ab12Cd",
	}
	argv := hop.resumeArgv(forward)
	joined := strings.Join(argv, "\n")
	if argv[0] != "ssh" || argv[1] != "-t" {
		t.Fatalf("argv head = %q %q", argv[0], argv[1])
	}
	if !strings.Contains(joined, "StreamLocalBindUnlink=yes") {
		t.Fatalf("missing unlink opt:\n%s", joined)
	}
	if !strings.Contains(joined, "/tmp/camp-hr-r.sock:/tmp/camp-hr-l.sock") {
		t.Fatalf("missing forward:\n%s", joined)
	}
	if strings.Contains(joined, "127.0.0.1:50123:") {
		t.Fatalf("the loopback forward belongs to the ControlMaster, not the interactive ssh:\n%s", joined)
	}
	remoteCmd := argv[len(argv)-1]
	for _, export := range []string{
		"export " + hopResumeSockEnv + "='/tmp/camp-hr-r.sock'",
		"export " + hopResumeAddrEnv + "='127.0.0.1:50123'",
		"export " + hopResumeTokenFileEnv + "='/tmp/camp-ht-Ab12Cd'",
	} {
		if !strings.Contains(remoteCmd, export) {
			t.Fatalf("remote command missing %q: %s", export, remoteCmd)
		}
	}
	if strings.Contains(joined, forward.token) {
		t.Fatalf("the token must never appear in a command line:\n%s", joined)
	}
	for name, withhold := range map[string]func(*hopResumeForward){
		"no token file":       func(f *hopResumeForward) { f.tokenFile = "" },
		"no verified forward": func(f *hopResumeForward) { f.loopAddr = "" },
	} {
		partial := forward
		withhold(&partial)
		withheld := strings.Join(hop.resumeArgv(partial), "\n")
		if strings.Contains(withheld, hopResumeAddrEnv) || strings.Contains(withheld, hopResumeTokenFileEnv) {
			t.Fatalf("%s must withhold the loopback route:\n%s", name, withheld)
		}
		if !strings.Contains(withheld, "/tmp/camp-hr-r.sock:/tmp/camp-hr-l.sock") {
			t.Fatalf("%s: the socket forward must stay:\n%s", name, withheld)
		}
	}

	open := loopForwardArgv(hop, "forward", forward.loopLocalSock)
	if got := strings.Join(open[len(open)-5:], " "); got != "-O forward -R 127.0.0.1:0:/tmp/camp-hr-t.sock lance@devbox.ts.net" {
		t.Fatalf("the server must pick the loopback port (port 0) on the hop's ControlMaster: %q", got)
	}
	if !strings.Contains(strings.Join(open, "\n"), "ControlPath=") {
		t.Fatalf("forward request lost the ControlMaster options: %q", open)
	}

	upload := hopTokenArgv(hop)
	if upload[0] != "-T" {
		t.Fatalf("token upload must refuse a pty so a forced-tty ssh config cannot swallow the end of input: %q", upload)
	}
	if strings.Contains(strings.Join(upload, "\n"), forward.token) {
		t.Fatalf("the token must never appear in a command line: %q", upload)
	}
	if !strings.Contains(remoteCmd, "cd '/srv/campaigns/obey' && exec $SHELL -l") &&
		!strings.Contains(remoteCmd, `cd '"'"'/srv/campaigns/obey'"'"'`) &&
		!strings.Contains(remoteCmd, "exec $SHELL -l") {
		t.Fatalf("remote command dropped the cd/exec: %s", remoteCmd)
	}
	if !strings.Contains(remoteCmd, "CAMP_HOP_ORIGIN") {
		t.Fatalf("remote command dropped the origin export: %s", remoteCmd)
	}
}

func TestResumeSwitchLineQuotesSelector(t *testing.T) {
	line := resumeSwitchLine("Festival Method Kit", cmdutil.CampaignScope{})
	if !strings.Contains(line, `'Festival Method Kit'`) || !strings.HasSuffix(line, "&& exit\n") {
		t.Fatalf("line = %q", line)
	}
}
