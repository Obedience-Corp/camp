package main

import (
	"errors"
	"slices"
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

func TestHopLineForwardsTheSocketAndNamesBothRoutes(t *testing.T) {
	var buf strings.Builder
	m := &machines.Machine{ID: "devbox", Host: "devbox.ts.net", SSHUser: "lance", AuthMethod: machines.AuthSSHAgent}
	if err := emitShellConnect(&buf, true, "/srv/campaigns/obey", remote.Direct(m), "v1;host=example"); err != nil {
		t.Fatal(err)
	}
	hop, err := splitHopLine(buf.String())
	if err != nil {
		t.Fatal(err)
	}
	argv := hop.resumeArgv("/tmp/camp-hr-l.sock", "/tmp/camp-hr-r.sock", "0123456789abcdef")
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
	for _, export := range []string{
		"export " + hopResumeSockEnv + "='/tmp/camp-hr-r.sock'",
		"export " + hopResumeRelayEnv + "='0123456789abcdef'",
	} {
		if !strings.Contains(remoteCmd, export) {
			t.Fatalf("remote command missing %q: %s", export, remoteCmd)
		}
	}
	if !strings.Contains(remoteCmd, "cd '/srv/campaigns/obey' && exec $SHELL -l") &&
		!strings.Contains(remoteCmd, `cd '"'"'/srv/campaigns/obey'"'"'`) &&
		!strings.Contains(remoteCmd, "exec $SHELL -l") {
		t.Fatalf("remote command dropped the cd/exec: %s", remoteCmd)
	}
	if !strings.Contains(remoteCmd, "CAMP_HOP_ORIGIN") {
		t.Fatalf("remote command dropped the origin export: %s", remoteCmd)
	}

	relay := hopRelayArgv(hop, "0123456789abcdef")
	if relay[0] != "-T" {
		t.Fatalf("the relay's ssh must refuse a pty, or a forced-tty ssh config turns its stream into a terminal: %q", relay)
	}
	own, hops := slices.Index(relay, "ControlMaster=no"), slices.Index(relay, "ControlMaster=auto")
	if own < 0 || hops < 0 || own > hops {
		t.Fatalf("the relay must never create the ControlMaster the interactive ssh is about to; ssh keeps the first value: %q", relay)
	}
	if !slices.ContainsFunc(relay, func(arg string) bool { return strings.HasPrefix(arg, "ControlPath=") }) {
		t.Fatalf("the relay should ride the hop's ControlMaster when one is up: %q", relay)
	}
	if relay[len(relay)-2] != "lance@devbox.ts.net" || !strings.Contains(relay[len(relay)-1], "hop-resume relay 0123456789abcdef") {
		t.Fatalf("the relay must run the far machine's camp on the hop's target: %q", relay[len(relay)-2:])
	}
	if slices.Contains(relay, "-t") || strings.Contains(strings.Join(relay, "\n"), "exec $SHELL -l") {
		t.Fatalf("the relay is not an interactive session: %q", relay)
	}
}

func TestResumeSwitchLineQuotesSelector(t *testing.T) {
	line := resumeSwitchLine("Festival Method Kit", cmdutil.CampaignScope{})
	if !strings.Contains(line, `'Festival Method Kit'`) || !strings.HasSuffix(line, "&& exit\n") {
		t.Fatalf("line = %q", line)
	}
}
