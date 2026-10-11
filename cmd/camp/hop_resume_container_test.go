//go:build container_fs

package main

import (
	"context"
	"encoding/json"
	"errors"
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

func TestHopResumeFSSocketRoundTrip(t *testing.T) {
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
		follow, _ := serveHopResume(context.Background(), ln, new(hopSwitch), func(_ context.Context, request hopResumeRequest) (string, error) {
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

func TestHopResumeFSCapturesHostBeforeShellEval(t *testing.T) {
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

func TestHopResumeFSOriginAdvisoryAliasCollision(t *testing.T) {
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

func TestHopResumeFSSendRejectsMissingOrMalformedAcknowledgement(t *testing.T) {
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

func TestHopResumeFSUsesParentAuthenticationAndScope(t *testing.T) {
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

// shortHome gives a test its own home directory with a path short enough for
// a unix socket beneath it.
func shortHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "hr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	return home
}

// runningHopRelay starts a relay as the far side of a hop would run it and
// returns its stream to the opening shell plus the function that ends the hop.
type runningHopRelay struct {
	fromFar io.Reader
	toFar   io.WriteCloser
	done    chan error
}

func startTestHopRelay(t *testing.T, id string) runningHopRelay {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	relay := runningHopRelay{fromFar: outR, toFar: inW, done: make(chan error, 1)}
	go func() {
		relay.done <- runHopRelay(context.Background(), id, inR, outW)
		_ = outW.Close()
	}()
	t.Cleanup(func() { _ = inW.Close() })
	path, err := hopRelaySocketPath(id)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSocket != 0 && info.Mode().Perm() == 0o600 {
			return relay
		}
		select {
		case err := <-relay.done:
			t.Fatalf("relay ended before it listened: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("relay never listened")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHopResumeFSRelayKeepsItsSocketPrivateAndRemovesIt(t *testing.T) {
	home := shortHome(t)
	dir := filepath.Join(home, ".obey", "hop-resume")
	// A directory an earlier tool left open to others must be closed again.
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	const id = "0123456789abcdef"
	relay := startTestHopRelay(t, id)
	path := filepath.Join(dir, id+".sock")
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("relay directory must be closed to other users: %v %v", info.Mode().Perm(), err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("relay socket must be closed to other users: %v %v", info.Mode().Perm(), err)
	}
	if err := trustedHopSocket(path); err != nil {
		t.Fatalf("the far shell must trust its own hop's relay: %v", err)
	}
	// The opening shell closing the stream is the end of the hop.
	_ = relay.toFar.Close()
	select {
	case err := <-relay.done:
		if err != nil {
			t.Fatalf("relay: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the relay outlived its hop")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("an ended relay must remove its socket: %v", err)
	}
}

func TestHopResumeFSRelayPrunesSocketsNothingListensOn(t *testing.T) {
	home := shortHome(t)
	dir := filepath.Join(home, ".obey", "hop-resume")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// One relay that was killed before it could clean up, one still serving.
	dead := filepath.Join(dir, "aaaaaaaaaaaaaaaa.sock")
	killed, err := net.Listen("unix", dead)
	if err != nil {
		t.Fatal(err)
	}
	killed.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = killed.Close()
	live := filepath.Join(dir, "bbbbbbbbbbbbbbbb.sock")
	serving, err := net.Listen("unix", live)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = serving.Close() }()
	go func() {
		for {
			conn, err := serving.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	startTestHopRelay(t, "0123456789abcdef")
	if _, err := os.Lstat(dead); !os.IsNotExist(err) {
		t.Fatalf("a socket nothing listens on must be pruned: %v", err)
	}
	if _, err := os.Lstat(live); err != nil {
		t.Fatalf("another hop's live relay must be left alone: %v", err)
	}
}

func TestHopResumeFSSendSwitchesOverARelay(t *testing.T) {
	shortHome(t)
	const id = "0123456789abcdef"
	relay := startTestHopRelay(t, id)
	hop := new(hopSwitch)
	follow := make(chan string, 1)
	go func() {
		line, _ := serveHopRelay(context.Background(), relay.fromFar, relay.toFar, hop, func(_ context.Context, request hopResumeRequest) (string, error) {
			if request.Selector != "thirdbox:notes" || request.Host != "c.example" || request.Scope.Org != "work" {
				return "", errors.New("request changed in transit")
			}
			return "ssh prepared-parent-route", nil
		})
		follow <- line
	}()
	// The forwarded socket as a root-binding ssh server leaves it: named in
	// the environment, closed to this shell.
	t.Setenv(hopResumeSockEnv, filepath.Join(t.TempDir(), "camp-hr-r.sock"))
	t.Setenv(hopResumeRelayEnv, id)
	if err := sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{Org: "work"}, "c.example"); err != nil {
		t.Fatalf("switch over the relay: %v", err)
	}
	select {
	case line := <-follow:
		if line != "ssh prepared-parent-route" {
			t.Fatalf("follow-up %q", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the confirmed switch was not queued")
	}

	// A shell that outlives its hop keeps the relay id and nothing else works.
	_ = relay.toFar.Close()
	<-relay.done
	err := sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example")
	if err == nil || !strings.Contains(err.Error(), "relay for this hop is not running") {
		t.Fatalf("after the hop ended the far shell must stay put and say why: %v", err)
	}
}

func TestHopResumeFSSendFallsBackToTheForwardedSocket(t *testing.T) {
	shortHome(t)
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
	follow := make(chan string, 1)
	go func() {
		line, _ := serveHopResume(context.Background(), ln, new(hopSwitch), func(_ context.Context, request hopResumeRequest) (string, error) {
			return "ssh " + request.Selector, nil
		})
		follow <- line
	}()
	// The opening shell offered a relay, but the camp ssh found on this side
	// predates relays, so none is running. sshd bound the socket as this user.
	t.Setenv(hopResumeRelayEnv, "0123456789abcdef")
	t.Setenv(hopResumeSockEnv, local)
	if err := sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example"); err != nil {
		t.Fatalf("the released socket route must keep working: %v", err)
	}
	if line := <-follow; line != "ssh thirdbox:notes" {
		t.Fatalf("follow-up %q", line)
	}
}

// A hopped shell can outlive its hop and still holds the names its environment
// gave it. The suite runs as root, which can hand a file to another user the
// way that user would have created it.
func TestHopResumeFSTrustsOnlySocketsThisUserOwns(t *testing.T) {
	const stranger = 54321
	listen := func(dir, name string) string {
		path := filepath.Join(dir, name)
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		return path
	}
	mkdir := func(mode os.FileMode) string {
		dir, err := os.MkdirTemp("/tmp", "hr")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	private, sticky, open := mkdir(0o700), mkdir(0o777|os.ModeSticky), mkdir(0o777)

	for name, path := range map[string]string{
		"own socket in the user's own directory":  listen(private, "own.sock"),
		"own socket in a sticky shared directory": listen(sticky, "own.sock"),
	} {
		if err := trustedHopSocket(path); err != nil {
			t.Errorf("%s must be trusted: %v", name, err)
		}
	}

	planted := listen(sticky, "planted.sock")
	if err := os.Chown(planted, stranger, stranger); err != nil {
		t.Fatal(err)
	}
	theirs := mkdir(0o755)
	inTheirs := listen(theirs, "own.sock")
	if err := os.Chown(theirs, stranger, stranger); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(private, "file.sock")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(private, "link.sock")
	if err := os.Symlink(filepath.Join(private, "own.sock"), link); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"a socket another user owns":                planted,
		"a socket where others can replace entries": listen(open, "own.sock"),
		"a socket in another user's directory":      inTheirs,
		"a regular file":                            file,
		"a symlink to a socket":                     link,
		"a missing path":                            filepath.Join(private, "gone.sock"),
	} {
		if err := trustedHopSocket(path); err == nil {
			t.Errorf("%s must not be trusted", name)
		}
	}

	// End to end: the stranger's socket answers at the forwarded path and
	// acknowledges at once, as a listener that wants the shell gone would.
	shortHome(t)
	reached := make(chan struct{}, 1)
	ln, err := net.Listen("unix", filepath.Join(sticky, "camp-hr-r.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		reached <- struct{}{}
		_, _ = io.WriteString(conn, `{"version":1,"accepted":true}`+"\n")
		_ = conn.Close()
	}()
	forwarded := filepath.Join(sticky, "camp-hr-r.sock")
	if err := os.Chown(forwarded, stranger, stranger); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(forwarded, 0o666); err != nil {
		t.Fatal(err)
	}
	t.Setenv(hopResumeSockEnv, forwarded)
	t.Setenv(hopResumeRelayEnv, "0123456789abcdef")
	err = sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example")
	if err == nil || !strings.Contains(err.Error(), "relay for this hop is not running") || !strings.Contains(err.Error(), "only this user could have put there") {
		t.Fatalf("the far shell must refuse both routes and name each reason: %v", err)
	}
	select {
	case <-reached:
		t.Fatal("the far shell connected to another user's socket")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHopResumeFSRelayRefusesADirectoryItDoesNotOwn(t *testing.T) {
	home := shortHome(t)
	dir := filepath.Join(home, ".obey", "hop-resume")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The suite runs as root, which can hand the directory to another user.
	if err := os.Chown(dir, 54321, 54321); err != nil {
		t.Fatal(err)
	}
	err := runHopRelay(context.Background(), "0123456789abcdef", strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "not this user's own directory") {
		t.Fatalf("a relay must not listen where another user owns the directory: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(dir, "0123456789abcdef.sock")); !os.IsNotExist(statErr) {
		t.Fatalf("no socket may be left behind: %v", statErr)
	}
}
