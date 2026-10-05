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
	argv, err := prepareResumeSSH(buf.String(), "/tmp/camp-hr-l.sock", "/tmp/camp-hr-r.sock")
	if err != nil {
		t.Fatal(err)
	}
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
	remoteCmd := argv[len(argv)-1]
	if !strings.Contains(remoteCmd, "export "+hopResumeSockEnv+"=") {
		t.Fatalf("remote command missing resume export: %s", remoteCmd)
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
