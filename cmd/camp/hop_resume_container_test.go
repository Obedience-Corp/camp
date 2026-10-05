//go:build container_fs

package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
)

func TestHopResumeSocketRoundTrip(t *testing.T) {
	local, _, err := resumeSocketPaths()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(local) })
	ln, err := net.Listen("unix", local)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	t.Setenv(hopResumeSockEnv, local)
	scope := cmdutil.CampaignScope{Org: "obey", Status: "inactive", All: true}
	done := make(chan string, 1)
	go func() {
		follow, _ := serveHopResume(context.Background(), ln, func(_ context.Context, request hopResumeRequest) (string, error) {
			if request.Scope != scope {
				t.Errorf("scope %+v", request.Scope)
			}
			mapped, err := mapHopResume(request, &machines.File{Machines: []machines.Machine{{ID: "parent-alias", Host: "c.example"}}})
			return mapped.Selector, err
		})
		done <- follow
	}()
	// A rejected request leaves the listener available for the next selection.
	if err := sendHopResume(context.Background(), "thirdbox:notes", scope, "unavailable.example"); err == nil || !strings.Contains(err.Error(), "no route") {
		t.Fatalf("rejection: %v", err)
	}
	if err := sendHopResume(context.Background(), "thirdbox:notes", scope, "c.example"); err != nil {
		t.Fatal(err)
	}
	if got := <-done; got != "parent-alias:notes" {
		t.Fatalf("followup %q", got)
	}
}

func TestHopResumeCapturesHostBeforeShellEval(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "machines.yaml")
	t.Setenv("CAMP_MACHINES_PATH", registry)
	save := func(host string) {
		t.Helper()
		if err := (&machines.File{Machines: []machines.Machine{{ID: "thirdbox", Host: host, AuthMethod: machines.AuthSSHAgent, SSHUser: "child", IdentityFile: "child.key"}}}).Save(); err != nil {
			t.Fatal(err)
		}
	}
	save("c.example")
	host, err := hopResumeTargetHost("thirdbox:notes")
	if err != nil {
		t.Fatal(err)
	}
	line := resumeSwitchLineForHost("thirdbox:notes", cmdutil.CampaignScope{Org: "work"}, host)
	save("d.example")
	if !strings.Contains(line, "--host 'c.example'") || strings.Contains(line, "child.key") || strings.Contains(line, "--host 'd.example'") {
		t.Fatalf("line %s", line)
	}
	request, err := newHopResumeRequest("thirdbox:notes", cmdutil.CampaignScope{}, host)
	if err != nil || request.Host != "c.example" {
		t.Fatalf("request %+v %v", request, err)
	}
}

func TestHopResumeOriginAdvisoryAliasCollision(t *testing.T) {
	t.Setenv("CAMP_MACHINES_PATH", filepath.Join(t.TempDir(), "machines.yaml"))
	origin := HopOrigin{Host: "origin.example", ID: "thirdbox", User: "origin"}
	file := &machines.File{Machines: []machines.Machine{{ID: "thirdbox", Host: "c.example", AuthMethod: machines.AuthSSHAgent}}}
	if err := file.Save(); err != nil {
		t.Fatal(err)
	}
	if selectorNamesOrigin("thirdbox", origin) {
		t.Fatal("child registry alias was overridden by advisory origin ID")
	}
	payload, err := encodeHopOrigin(origin)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(HopOriginEnvVar, payload)
	t.Setenv("SSH_CONNECTION", "fixture")
	selector, err := cmdutil.ParseMachineSelector("thirdbox:notes")
	if err != nil {
		t.Fatal(err)
	}
	if resume, unwind := unwindInsteadOfHop(selector); !unwind || resume != "thirdbox:notes" {
		t.Fatalf("resume=%q unwind=%v", resume, unwind)
	}
}

func TestHopResumeSendRejectsMissingOrMalformedAcknowledgement(t *testing.T) {
	for _, reply := range []string{"", "{}", `{ "version":2,"accepted":true }`, `{ "version":1,"accepted":false }`, "invalid"} {
		t.Run(reply, func(t *testing.T) {
			local, _, err := resumeSocketPaths()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Remove(local) })
			ln, err := net.Listen("unix", local)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ln.Close() }()
			t.Setenv(hopResumeSockEnv, local)
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				defer func() { _ = conn.Close() }()
				var request hopResumeEnvelope
				_ = json.NewDecoder(conn).Decode(&request)
				_, _ = io.WriteString(conn, reply)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := sendHopResume(ctx, "thirdbox:notes", cmdutil.CampaignScope{}, "c.example"); err == nil {
				t.Fatal("accepted invalid acknowledgement")
			}
			<-done
		})
	}
}

func TestHopResumeUsesParentAuthenticationAndScope(t *testing.T) {
	t.Setenv("CAMP_MACHINES_PATH", filepath.Join(t.TempDir(), "machines.yaml"))
	parent := machines.Machine{ID: "parent-alias", Host: "192.0.2.3", AuthMethod: machines.AuthSSHAgent, SSHUser: "parent-user", IdentityFile: "parent.key"}
	registry := &machines.File{Machines: []machines.Machine{{ID: "thirdbox", Host: "192.0.2.4", AuthMethod: machines.AuthTailscaleSSH}, parent}}
	if err := registry.Save(); err != nil {
		t.Fatal(err)
	}
	scope := cmdutil.CampaignScope{Org: "work", Status: "inactive"}
	request, err := mapHopResume(hopResumeRequest{Selector: "thirdbox:work/notes@p", Host: "192.0.2.3", Scope: scope}, registry)
	if err != nil {
		t.Fatal(err)
	}
	original := resolveRemoteRootScoped
	t.Cleanup(func() { resolveRemoteRootScoped = original })
	called := false
	resolveRemoteRootScoped = func(_ context.Context, machine *machines.Machine, selector string, got remote.SwitchScope) (string, error) {
		called = true
		if *machine != parent || selector != "work/notes@p" || got.Org != scope.Org || got.Status != scope.Status || got.All != scope.All {
			t.Errorf("wrong parent route: %+v %s %+v", machine, selector, got)
		}
		return "/fixture/notes/projects", nil
	}
	selector, err := cmdutil.ParseMachineSelector(request.Selector)
	if err != nil {
		t.Fatal(err)
	}
	cmd, _ := fleetReviewCmd(scope, false)
	err = runRemoteSwitch(context.Background(), cmd, selector, false, false, false)
	if !called || err == nil || !strings.Contains(err.Error(), "resolved parent-alias:work/notes@p") {
		t.Fatalf("resolution called=%v err=%v", called, err)
	}
}
