//go:build container_fs && !windows

package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
)

func TestHopResumeFSSocketRoundTrip(t *testing.T) {
	if asOrdinaryUser(t) {
		return
	}
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
	if asOrdinaryUser(t) {
		return
	}
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

// What the opening shell says when it refuses a route is shown in the far
// terminal, on the forwarded-socket route too, so it must not drive it.
func TestHopResumeFSSendShowsARefusalWithoutItsControlCharacters(t *testing.T) {
	if asOrdinaryUser(t) {
		return
	}
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
	refusal, err := json.Marshal(hopResumeResponse{Version: 1, Error: "\x1b]0;owned\x07parent has no route"})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var request hopResumeEnvelope
		_ = json.NewDecoder(conn).Decode(&request)
		_, _ = conn.Write(refusal)
	}()
	t.Setenv(hopResumeSockEnv, local)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = sendHopResume(ctx, "thirdbox:notes", cmdutil.CampaignScope{}, "c.example")
	if err == nil || strings.ContainsAny(err.Error(), "\x1b\x07") || !strings.Contains(err.Error(), "parent has no route") {
		t.Fatalf("a refusal must be shown without its control characters: %q", err)
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

// ordinaryUID is a user that is neither root nor anyone the image knows.
const ordinaryUID = 54321

// asOrdinaryUser reruns the calling test in a child process that is not root,
// and reports whether it did. The suite runs as root, and what a hopped shell
// trusts depends on who it is: root must not trust a shared directory at all,
// so the forwarded socket in /tmp can only be exercised as an ordinary user.
// env carries fixtures only root could prepare.
func asOrdinaryUser(t *testing.T, env ...string) bool {
	t.Helper()
	if os.Geteuid() != 0 {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.MkdirTemp("/tmp", "hu")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	if err := os.Chown(home, ordinaryUID, ordinaryUID); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^"+strings.SplitN(t.Name(), "/", 2)[0]+"$", "-test.count=1", "-test.v")
	cmd.Dir = home
	cmd.Env = append(append(os.Environ(), "HOME="+home, "TMPDIR="+home), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: ordinaryUID, Gid: ordinaryUID}}
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: ") {
		t.Fatalf("as an ordinary user: %v\n%s", err, out)
	}
	return true
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

func startTestHopRelay(t *testing.T, id, forwardedSock string) runningHopRelay {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	relay := runningHopRelay{fromFar: outR, toFar: inW, done: make(chan error, 1)}
	go func() {
		relay.done <- runHopRelay(context.Background(), id, forwardedSock, inR, outW)
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
	relay := startTestHopRelay(t, id, "")
	go func() { _, _ = io.Copy(io.Discard, relay.fromFar) }()
	path, err := hopRelaySocketPath(id)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("relay directory must be closed to other users: %v %v", info.Mode().Perm(), err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("relay socket must be closed to other users: %v %v", info.Mode().Perm(), err)
	}
	if err := trustedHopSocket(path); err != nil {
		t.Fatalf("the far shell must trust its own hop's relay: %v", err)
	}
	note := hopRelayHostPath(path)
	if info, err := os.Lstat(note); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the note of which machine the relay runs on: %v %v", info, err)
	}
	if tag, err := os.ReadFile(note); err != nil || string(tag) != hopHostTag() {
		t.Fatalf("the note must name this machine: %q %v", tag, err)
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
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("an ended relay must leave nothing behind: %v %v", entries, err)
	}
}

func TestHopResumeFSRelayPrunesSocketsNothingListensOn(t *testing.T) {
	home := shortHome(t)
	dir := filepath.Join(home, ".obey", "hop-resume")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	longAgo := time.Now().Add(-time.Hour)
	// abandon leaves what a killed relay leaves: a socket nothing listens on
	// and, beside it, the note of the machine it ran on.
	abandon := func(id, machine string, at time.Time) string {
		path := filepath.Join(dir, id+".sock")
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		ln.(*net.UnixListener).SetUnlinkOnClose(false)
		_ = ln.Close()
		if machine != "" {
			if err := os.WriteFile(hopRelayHostPath(path), []byte(machine), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
		return path
	}
	here := hopHostTag()
	dead := abandon("aaaaaaaaaaaaaaaa", here, longAgo)
	// One whose socket is seconds old may be about to listen.
	starting := abandon("cccccccccccccccc", here, time.Now())
	// The same home directory can be mounted on another machine, whose live
	// relay refuses a connection made from here.
	elsewhere := abandon("dddddddddddddddd", "another machine", longAgo)
	unnoted := abandon("eeeeeeeeeeeeeeee", "", longAgo)
	live := filepath.Join(dir, "bbbbbbbbbbbbbbbb.sock")
	serving, err := net.Listen("unix", live)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = serving.Close() }()
	if err := os.WriteFile(hopRelayHostPath(live), []byte(here), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(live, longAgo, longAgo); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := serving.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	relay := startTestHopRelay(t, "0123456789abcdef", "")
	go func() { _, _ = io.Copy(io.Discard, relay.fromFar) }()
	if _, err := os.Lstat(dead); !os.IsNotExist(err) {
		t.Fatalf("an old socket nothing listens on must be pruned: %v", err)
	}
	if _, err := os.Lstat(hopRelayHostPath(dead)); !os.IsNotExist(err) {
		t.Fatalf("a pruned socket's note must go with it: %v", err)
	}
	if _, err := os.Lstat(starting); err != nil {
		t.Fatalf("a socket created moments ago may belong to a relay still starting: %v", err)
	}
	if _, err := os.Lstat(live); err != nil {
		t.Fatalf("another hop's live relay must be left alone: %v", err)
	}
	if _, err := os.Lstat(elsewhere); err != nil {
		t.Fatalf("a relay socket that belongs to another machine must be left alone: %v", err)
	}
	if _, err := os.Lstat(unnoted); err != nil {
		t.Fatalf("a socket nothing says belongs to this machine must be left alone: %v", err)
	}
	// A note that is a link names whatever the link leads to, which can
	// change, so it is not a note. The file it leads to must be untouched.
	decoyTarget := filepath.Join(dir, "decoy-tag")
	if err := os.WriteFile(decoyTarget, []byte(here), 0o600); err != nil {
		t.Fatal(err)
	}
	decoy := abandon("ffffffffffffffff", "", longAgo)
	if err := os.Symlink(decoyTarget, hopRelayHostPath(decoy)); err != nil {
		t.Fatal(err)
	}
	relay = startTestHopRelay(t, "0123456789abcdee", "")
	go func() { _, _ = io.Copy(io.Discard, relay.fromFar) }()
	if _, err := os.Lstat(decoy); err != nil {
		t.Fatalf("a socket whose note is a symlink must be left alone: %v", err)
	}
	if got, err := os.ReadFile(decoyTarget); err != nil || string(got) != here {
		t.Fatalf("a symlink note must not be followed: %q %v", got, err)
	}
}

// A directory left open to others can be given a file where the socket goes
// and a symlink where the note goes, named for a relay id read off the
// process list. Neither may be followed or deferred to.
func TestHopResumeFSRelayReplacesAPlantedPathAndDoesNotFollowItsNote(t *testing.T) {
	home := shortHome(t)
	dir := filepath.Join(home, ".obey", "hop-resume")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	const id = "0123456789abcdef"
	path := filepath.Join(dir, id+".sock")
	if err := os.WriteFile(path, []byte("not a socket"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "note-target")
	const sentinel = "sentinel"
	if err := os.WriteFile(target, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, hopRelayHostPath(path)); err != nil {
		t.Fatal(err)
	}

	relay := startTestHopRelay(t, id, "")
	go func() { _, _ = io.Copy(io.Discard, relay.fromFar) }()
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("a planted file must not keep the relay from listening: %v %v", info, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != sentinel {
		t.Fatalf("the relay followed its note symlink and wrote %q: %v", got, err)
	}
	if info, err := os.Lstat(hopRelayHostPath(path)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the note symlink must still be a symlink: %v %v", info, err)
	}
}

// A fifo is not a note. prune reads the note beside an old socket, and the
// relay writes its own on the way out; opening either fifo would block, so
// no later relay would start. Both are left as fifos, and nothing is written
// into the one this relay would have used.
func TestHopResumeFSRelayFIFONoteDoesNotBlockPruneOrWrite(t *testing.T) {
	home := shortHome(t)
	const id = "0123456789abcdef"
	path, err := hopRelaySocketPath(id)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".obey", "hop-resume")
	if filepath.Dir(path) != dir {
		t.Fatalf("relay socket %s is not under %s", path, dir)
	}
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	// An old socket whose note is a fifo with no writer. Reading it blocks.
	dead := filepath.Join(dir, "aaaaaaaaaaaaaaaa.sock")
	ln, err := net.Listen("unix", dead)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = ln.Close()
	deadNote := hopRelayHostPath(dead)
	if err := syscall.Mkfifo(deadNote, 0o600); err != nil {
		t.Fatal(err)
	}
	longAgo := time.Now().Add(-time.Hour)
	if err := os.Chtimes(dead, longAgo, longAgo); err != nil {
		t.Fatal(err)
	}
	// This relay's own note is a fifo a reader already holds. Opening it for
	// writing would either block or leave the host tag in the fifo. The
	// relay must do neither, and must still report that it is listening.
	ownNote := hopRelayHostPath(path)
	if err := syscall.Mkfifo(ownNote, 0o600); err != nil {
		t.Fatal(err)
	}
	noteReader, err := os.OpenFile(ownNote, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = noteReader.Close() })

	relay := startTestHopRelay(t, id, "")
	ready := make(chan string, 1)
	go func() {
		reader := bufio.NewReader(relay.fromFar)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				ready <- ""
				return
			}
			if strings.Contains(line, `"ready":true`) {
				ready <- line
				return
			}
		}
	}()
	select {
	case line := <-ready:
		if !strings.Contains(line, `"ready":true`) {
			t.Fatal("a fifo note blocked writing the relay note")
		}
	case err := <-relay.done:
		t.Fatalf("relay ended before its note was written: %v", err)
	case <-time.After(time.Second):
		t.Fatal("a fifo note blocked writing the relay note")
	}

	if _, err := os.Lstat(dead); err != nil {
		t.Fatalf("a socket whose note is a fifo must be left alone: %v", err)
	}
	for _, note := range []string{deadNote, ownNote} {
		info, err := os.Lstat(note)
		if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			t.Fatalf("a fifo note must still be a fifo: %s %v %v", note, info, err)
		}
	}
	buf := make([]byte, 64)
	n, err := noteReader.Read(buf)
	if n != 0 || (err != nil && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, io.EOF)) {
		got := ""
		if n > 0 {
			got = string(buf[:n])
		}
		t.Fatalf("the relay wrote into a fifo note: %q %v", got, err)
	}
}

// A relay that is started again for the same hop replaces one the opening
// shell gave up on. That one may still be running, with nobody on its link.
func TestHopResumeFSRestartedRelayTakesOverItsSocket(t *testing.T) {
	shortHome(t)
	const id = "0123456789abcdef"
	path, err := hopRelaySocketPath(id)
	if err != nil {
		t.Fatal(err)
	}
	first := startTestHopRelay(t, id, "")
	go func() { _, _ = io.Copy(io.Discard, first.fromFar) }()
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}

	second := startTestHopRelay(t, id, "")
	select {
	case err := <-second.done:
		t.Fatalf("the relay started again must not defer to the one it replaces: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if after, err := os.Lstat(path); err != nil || os.SameFile(before, after) {
		t.Fatalf("the relay started again must listen at the hop's path itself: %v", err)
	}
	ctx, endHop := context.WithCancel(context.Background())
	defer endHop()
	follow := make(chan string, 1)
	go func() {
		line, _ := serveHopRelay(ctx, second.fromFar, second.toFar, new(hopSwitch), func(context.Context, hopResumeRequest) (string, error) {
			return "over the relay started again", nil
		})
		follow <- line
	}()

	// The first one learns only now that its hop gave up on it. The socket at
	// the path is no longer its own to remove.
	_ = first.toFar.Close()
	select {
	case <-first.done:
	case <-time.After(3 * time.Second):
		t.Fatal("the replaced relay did not end")
	}
	t.Setenv(hopResumeRelayEnv, id)
	t.Setenv(hopResumeSockEnv, "")
	if err := sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example"); err != nil {
		t.Fatalf("switch over the relay started again: %v", err)
	}
	endHop()
	if line := <-follow; line != "over the relay started again" {
		t.Fatalf("follow-up %q", line)
	}
}

// The relay starts a moment after the hop does. A far shell that asks in that
// moment waits for it instead of failing.
func TestHopResumeFSSendWaitsForARelayThatIsStillStarting(t *testing.T) {
	shortHome(t)
	const id = "0123456789abcdef"
	t.Setenv(hopResumeRelayEnv, id)
	t.Setenv(hopResumeSockEnv, "")
	sent := make(chan error, 1)
	go func() {
		sent <- sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example")
	}()
	select {
	case err := <-sent:
		t.Fatalf("the far shell gave up before the relay could start: %v", err)
	case <-time.After(1500 * time.Millisecond):
	}
	relay := startTestHopRelay(t, id, "")
	ctx, endHop := context.WithCancel(context.Background())
	defer endHop()
	go func() {
		_, _ = serveHopRelay(ctx, relay.fromFar, relay.toFar, new(hopSwitch), func(context.Context, hopResumeRequest) (string, error) {
			return "ssh prepared-parent-route", nil
		})
	}()
	select {
	case err := <-sent:
		if err != nil {
			t.Fatalf("switch over a relay that came up while the far shell waited: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the far shell never reached the relay")
	}
}

func TestHopResumeFSSendSwitchesOverARelay(t *testing.T) {
	shortHome(t)
	const id = "0123456789abcdef"
	relay := startTestHopRelay(t, id, "")
	hop := new(hopSwitch)
	ctx, endHop := context.WithCancel(context.Background())
	defer endHop()
	follow := make(chan string, 1)
	go func() {
		line, _ := serveHopRelay(ctx, relay.fromFar, relay.toFar, hop, func(_ context.Context, request hopResumeRequest) (string, error) {
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
	// The far shell exits, and with it the hop.
	endHop()
	select {
	case line := <-follow:
		if line != "ssh prepared-parent-route" {
			t.Fatalf("follow-up %q", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the switch was not handed to the opening shell")
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
	if asOrdinaryUser(t) {
		return
	}
	home := shortHome(t)
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
	hop := new(hopSwitch)
	served := func() <-chan string {
		follow := make(chan string, 1)
		go func() {
			line, _ := serveHopResume(context.Background(), ln, hop, func(_ context.Context, request hopResumeRequest) (string, error) {
				return "ssh " + request.Selector, nil
			})
			follow <- line
		}()
		return follow
	}
	// sshd bound the socket as this user. The opening shell offered a relay,
	// but none is running: the camp ssh found on this side predates relays.
	const id = "0123456789abcdef"
	t.Setenv(hopResumeRelayEnv, id)
	t.Setenv(hopResumeSockEnv, local)
	follow := served()
	if err := sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example"); err != nil {
		t.Fatalf("the released socket route must keep working: %v", err)
	}
	if line := <-follow; line != "ssh thirdbox:notes" {
		t.Fatalf("follow-up %q", line)
	}

	// The same when a relay was killed and left its socket behind.
	dir := filepath.Join(home, ".obey", "hop-resume")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	relayPath, err := hopRelaySocketPath(id)
	if err != nil {
		t.Fatal(err)
	}
	killed, err := net.Listen("unix", relayPath)
	if err != nil {
		t.Fatal(err)
	}
	killed.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = killed.Close()
	hop.release()
	follow = served()
	if err := sendHopResume(context.Background(), "fourthbox:notes", cmdutil.CampaignScope{}, "c.example"); err != nil {
		t.Fatalf("a dead relay socket must not block the socket route: %v", err)
	}
	if line := <-follow; line != "ssh fourthbox:notes" {
		t.Fatalf("follow-up %q", line)
	}
}

// acknowledging accepts on ln and answers every connection at once, the way a
// listener that wants a hopped shell gone would. It reports each connection.
func acknowledging(ln net.Listener) <-chan struct{} {
	reached := make(chan struct{}, 8)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			reached <- struct{}{}
			_, _ = io.WriteString(conn, `{"version":1,"accepted":true}`+"\n")
			_ = conn.Close()
		}
	}()
	return reached
}

// A hopped shell can outlive its hop and still holds the names its environment
// gave it. Root prepares what another user or a root-binding ssh server would
// leave in a shared directory; the ordinary user's shell must refuse it.
func TestHopResumeFSUserTrustsOnlyItsOwnSockets(t *testing.T) {
	const stickyEnv, plantedEnv, borrowedEnv = "CAMP_TEST_STICKY", "CAMP_TEST_PLANTED", "CAMP_TEST_BORROWED"
	if os.Geteuid() == 0 {
		sticky, err := os.MkdirTemp("/tmp", "hs")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(sticky) })
		if err := os.Chmod(sticky, 0o777|os.ModeSticky); err != nil {
			t.Fatal(err)
		}
		// Bound by root at a path of someone's choosing, open to all.
		planted := filepath.Join(sticky, "planted.sock")
		plantedLn, err := net.Listen("unix", planted)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = plantedLn.Close() }()
		if err := os.Chmod(planted, 0o666); err != nil {
			t.Fatal(err)
		}
		plantedReached := acknowledging(plantedLn)
		// A name that is the user's, answered by somebody else's process.
		borrowed := filepath.Join(sticky, "borrowed.sock")
		borrowedLn, err := net.Listen("unix", borrowed)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = borrowedLn.Close() }()
		if err := os.Chown(borrowed, ordinaryUID, ordinaryUID); err != nil {
			t.Fatal(err)
		}
		borrowedReached := acknowledging(borrowedLn)

		asOrdinaryUser(t, stickyEnv+"="+sticky, plantedEnv+"="+planted, borrowedEnv+"="+borrowed)
		select {
		case <-plantedReached:
			t.Fatal("the user's shell connected to a socket it does not own")
		default:
		}
		select {
		case <-borrowedReached:
		default:
			t.Fatal("the borrowed socket was never dialed, so the peer check was not what refused it")
		}
		return
	}

	sticky, planted, borrowed := os.Getenv(stickyEnv), os.Getenv(plantedEnv), os.Getenv(borrowedEnv)
	home := shortHome(t)
	listen := func(dir, name string) string {
		path := filepath.Join(dir, name)
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		return path
	}
	open, err := os.MkdirTemp(home, "open")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o777); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"own socket in the user's own directory":  listen(home, "own.sock"),
		"own socket in a sticky shared directory": listen(sticky, "own.sock"),
	} {
		if err := trustedHopSocket(path); err != nil {
			t.Errorf("%s must be trusted: %v", name, err)
		}
	}
	file := filepath.Join(home, "file.sock")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link.sock")
	if err := os.Symlink(filepath.Join(home, "own.sock"), link); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"a socket another user owns":                planted,
		"a socket where others can replace entries": listen(open, "own.sock"),
		"a regular file":                            file,
		"a symlink to a socket":                     link,
		"a missing path":                            filepath.Join(home, "gone.sock"),
	} {
		if err := trustedHopSocket(path); err == nil {
			t.Errorf("%s must not be trusted", name)
		}
	}

	// End to end, with no relay running: neither the planted socket nor the
	// borrowed one may end this shell, though both acknowledge at once.
	t.Setenv(hopResumeRelayEnv, "0123456789abcdef")
	t.Setenv(hopResumeSockEnv, planted)
	err = sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example")
	if err == nil || !strings.Contains(err.Error(), "relay for this hop is not running") || !strings.Contains(err.Error(), "only this user could have put there") {
		t.Fatalf("the far shell must refuse both routes and name each reason: %v", err)
	}
	t.Setenv(hopResumeSockEnv, borrowed)
	err = sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example")
	if err == nil || !strings.Contains(err.Error(), "runs as another user") {
		t.Fatalf("a socket answered by another user's process must be refused after connecting: %v", err)
	}
}

// A root-binding ssh server creates root-owned sockets in /tmp for any user,
// so to a root shell "a socket I own there" proves nothing.
func TestHopResumeFSRootNeverTrustsASharedDirectory(t *testing.T) {
	listen := func(dir, name string) (string, net.Listener) {
		path := filepath.Join(dir, name)
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		return path, ln
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
	private, sticky := mkdir(0o700), mkdir(0o777|os.ModeSticky)
	own, _ := listen(private, "own.sock")
	if err := trustedHopSocket(own); err != nil {
		t.Fatalf("root's socket in root's own directory must be trusted: %v", err)
	}
	forwarded, forwardedLn := listen(sticky, "camp-hr-r.sock")
	if err := trustedHopSocket(forwarded); err == nil {
		t.Fatal("root must not trust a root-owned socket in a shared directory")
	}
	foreign, _ := listen(private, "foreign.sock")
	if err := os.Chown(foreign, ordinaryUID, ordinaryUID); err != nil {
		t.Fatal(err)
	}
	if err := trustedHopSocket(foreign); err == nil {
		t.Fatal("a socket another user owns must not be trusted")
	}
	theirs := mkdir(0o755)
	inTheirs, _ := listen(theirs, "own.sock")
	if err := os.Chown(theirs, ordinaryUID, ordinaryUID); err != nil {
		t.Fatal(err)
	}
	if err := trustedHopSocket(inTheirs); err == nil {
		t.Fatal("a socket in another user's directory must not be trusted")
	}

	// End to end: a root shell that outlived its hop, with only the forwarded
	// name left, must not be ended by whatever answers there.
	shortHome(t)
	reached := acknowledging(forwardedLn)
	t.Setenv(hopResumeRelayEnv, "0123456789abcdef")
	t.Setenv(hopResumeSockEnv, forwarded)
	err := sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example")
	if err == nil || !strings.Contains(err.Error(), "only this user could have put there") {
		t.Fatalf("root must stay in its shell: %v", err)
	}
	select {
	case <-reached:
		t.Fatal("root connected to a socket in a shared directory")
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
	if err := os.Chown(dir, ordinaryUID, ordinaryUID); err != nil {
		t.Fatal(err)
	}
	err := runHopRelay(context.Background(), "0123456789abcdef", "", strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "not this user's own directory") {
		t.Fatalf("a relay must not listen where another user owns the directory: %v", err)
	}
	if entries, readErr := os.ReadDir(dir); readErr != nil || len(entries) != 0 {
		t.Fatalf("no socket may be left behind: %v %v", entries, readErr)
	}

	// Nor through a link, even to a directory of this user's own: where a
	// link leads can change.
	home = shortHome(t)
	elsewhere := filepath.Join(home, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".obey"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(home, ".obey", "hop-resume")); err != nil {
		t.Fatal(err)
	}
	err = runHopRelay(context.Background(), "0123456789abcdef", "", strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "not this user's own directory") {
		t.Fatalf("a relay must not listen through a link: %v", err)
	}
	if entries, readErr := os.ReadDir(elsewhere); readErr != nil || len(entries) != 0 {
		t.Fatalf("no socket may be left behind where the link led: %v %v", entries, readErr)
	}
}

// Where the forwarded socket already serves this user, the relay must not
// linger: the far shell keeps the route it has always had, and the relay does
// not hold one of the connection's sessions for the life of the hop.
func TestHopResumeFSRelayStepsAsideWhereTheSocketServes(t *testing.T) {
	const plantedEnv = "CAMP_TEST_PLANTED"
	if os.Geteuid() == 0 {
		// What a root-binding ssh server leaves at the forwarded path.
		sticky, err := os.MkdirTemp("/tmp", "hs")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(sticky) })
		if err := os.Chmod(sticky, 0o777|os.ModeSticky); err != nil {
			t.Fatal(err)
		}
		planted := filepath.Join(sticky, "camp-hr-r.sock")
		ln, err := net.Listen("unix", planted)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ln.Close() }()
		asOrdinaryUser(t, plantedEnv+"="+planted)
		return
	}
	home := shortHome(t)
	own, _, err := resumeSocketPaths()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(own) })
	ln, err := net.Listen("unix", own)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	// sshd bound the socket as this user: nothing for a relay to add. Its
	// input stays open, as it does for the life of a real hop, so only
	// stepping aside can end it.
	hopIn, endHop := io.Pipe()
	defer func() { _ = endHop.Close() }()
	done := make(chan error, 1)
	go func() {
		done <- runHopRelay(context.Background(), "0123456789abcdef", own, hopIn, io.Discard)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("relay: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the relay stayed although the forwarded socket serves this user")
	}
	if entries, _ := os.ReadDir(filepath.Join(home, ".obey", "hop-resume")); len(entries) != 0 {
		t.Fatalf("a relay that stepped aside must leave nothing behind: %v", entries)
	}

	// The same call where the server bound the socket as root: the relay is
	// the only route and must stay.
	relay := startTestHopRelay(t, "fedcba9876543210", os.Getenv(plantedEnv))
	go func() { _, _ = io.Copy(io.Discard, relay.fromFar) }()
	select {
	case err := <-relay.done:
		t.Fatalf("the relay left although the forwarded socket is closed to this user: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHopResumeFSSendPrefersTheRelayWhenBothRoutesAreUp(t *testing.T) {
	if asOrdinaryUser(t) {
		return
	}
	shortHome(t)
	const id = "0123456789abcdef"
	relay := startTestHopRelay(t, id, "")
	hop := new(hopSwitch)
	ctx, endHop := context.WithCancel(context.Background())
	defer endHop()
	follow := make(chan string, 1)
	go func() {
		line, _ := serveHopRelay(ctx, relay.fromFar, relay.toFar, hop, func(_ context.Context, request hopResumeRequest) (string, error) {
			return "over the relay", nil
		})
		follow <- line
	}()
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
	reached := acknowledging(ln)
	t.Setenv(hopResumeRelayEnv, id)
	t.Setenv(hopResumeSockEnv, local)
	if err := sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	endHop()
	if line := <-follow; line != "over the relay" {
		t.Fatalf("follow-up %q", line)
	}
	select {
	case <-reached:
		t.Fatal("a running relay must carry the switch, with its confirmation, rather than the socket")
	default:
	}
}

// TestHopResumeFSFarShellHelper is the far shell in the lifecycle test below:
// the fake ssh runs this binary with the hop's routes in its environment.
func TestHopResumeFSFarShellHelper(t *testing.T) {
	selector := os.Getenv("CAMP_TEST_FAR_SHELL")
	if selector == "" {
		return
	}
	if err := sendHopResume(context.Background(), selector, cmdutil.CampaignScope{}, "c.example"); err != nil {
		t.Fatalf("far shell: %v", err)
	}
}

// TestHopResumeFSRelayHelper is the far relay in the lifecycle test below: the
// fake ssh runs this binary in place of `camp hop-resume relay <id>`. What the
// test framework prints around the frames stands in for a noisy login shell.
func TestHopResumeFSRelayHelper(t *testing.T) {
	id := os.Getenv("CAMP_TEST_RELAY_ID")
	if id == "" {
		return
	}
	if err := runHopRelay(context.Background(), id, "", os.Stdin, os.Stdout); err != nil {
		t.Fatalf("relay: %v", err)
	}
}

const fakeHopSSH = `#!/bin/sh
# Stands in for ssh: records how it was called, then plays the hop or the relay.
case " $* " in
*" hop-resume relay "*)
	echo relay >> "$FAKE_SSH_LOG"
	starts=$(grep -c '^relay$' "$FAKE_SSH_LOG")
	echo "$$" > "$FAKE_SSH_LOG.relay-pid"
	printf '%s\n' "$@" > "$FAKE_SSH_LOG.relay-argv"
	readlink "/proc/$$/fd/2" > "$FAKE_SSH_LOG.relay-stderr"
	id=$(printf '%s' "$*" | sed -n 's/.*hop-resume relay \([0-9a-f]\{16\}\).*/\1/p')
	case "$FAKE_SSH_RELAY:$starts" in
	aside:*) exit 0 ;;
	predates:*) echo 'Error: unknown command "relay" for "camp hop-resume"' >&2; exit 1 ;;
	unreachable:*|unreachable-once:1) exit 255 ;;
	lost-once:1) printf '\n{"version":2,"ready":true}\n'; sleep 0.2; exit 1 ;;
	real:*|unreachable-once:*|lost-once:*) CAMP_TEST_RELAY_ID="$id" exec "$FAKE_SSH_SELF" -test.run '^TestHopResumeFSRelayHelper$' -test.v ;;
	*) exec cat > /dev/null ;;
	esac
	;;
*)
	echo hop >> "$FAKE_SSH_LOG"
	prev=""
	for arg in "$@"; do
		if [ "$prev" = "-R" ]; then spec=$arg; fi
		prev=$arg
	done
	if [ -n "$CAMP_TEST_FAR_SHELL" ]; then
		sleep "$FAKE_SSH_FAR_DELAY"
		if [ "$FAKE_SSH_FAR_ROUTE" = relay ]; then
			sock=""
			relay=$(printf '%s' "$*" | sed -n "s/.*CAMP_HOP_RESUME_RELAY='\([0-9a-f]*\)'.*/\1/p")
		else
			sock=${spec#*:}
			relay=""
		fi
		CAMP_HOP_RESUME_SOCK="$sock" CAMP_HOP_RESUME_RELAY="$relay" "$FAKE_SSH_SELF" -test.run '^TestHopResumeFSFarShellHelper$' > "$FAKE_SSH_LOG.far" 2>&1 || echo far-shell-failed >> "$FAKE_SSH_LOG"
	fi
	# A hop that is killed must leave nothing running, so where the wait can
	# be this process itself, it is.
	if [ "$FAKE_SSH_HOP_EXIT" = 0 ]; then exec sleep "$FAKE_SSH_HOP_SECONDS"; fi
	sleep "$FAKE_SSH_HOP_SECONDS"
	exit "$FAKE_SSH_HOP_EXIT"
	;;
esac
`

// sleeping reports whether a `sleep <seconds>` is running, which is how the
// fake ssh waits out a hop. Only the end of the command line is compared: an
// image run under emulation has the emulator's own arguments in front.
func sleeping(seconds string) bool {
	entries, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, entry := range entries {
		if cmdline, err := os.ReadFile(entry); err == nil && strings.HasSuffix(string(cmdline), "sleep\x00"+seconds+"\x00") {
			return true
		}
	}
	return false
}

// The hop from start to finish, with ssh replaced by a script: what the
// opening shell is left with, how long it is held, and what is left running.
func TestHopResumeFSRunCarriesAHopFromStartToFinish(t *testing.T) {
	if asOrdinaryUser(t) {
		return
	}
	shortHome(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(fakeHopSSH), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_SSH_SELF", self)
	around := hopSurroundings{
		prepare: func(_ context.Context, request hopResumeRequest) (string, error) {
			return "cd -- '/next/" + request.Selector + "'\n", nil
		},
		terminal: func() (*os.File, *os.File, bool, func(), error) {
			in, err := os.Open(os.DevNull)
			if err != nil {
				return nil, nil, false, nil, err
			}
			out, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if err != nil {
				return nil, nil, false, nil, err
			}
			return in, out, true, func() { _ = in.Close(); _ = out.Close() }, nil
		},
	}
	const line = "ssh -t '-o' 'BatchMode=yes' 'far.example' 'exec $SHELL -l'"
	const next = "cd -- '/next/thirdbox:notes'\n"
	type hopCase struct {
		hopSeconds, hopExit string
		farShell, farRoute  string // a far shell that asks to switch, and by which route
		farDelay            string
		relay               string // how the far relay behaves
		cancelAfter         time.Duration
		wantFollow          string
		wantErr             bool
		wantRelays          int // relay starts expected; -1 for "none unless the hop ran long"
	}
	for name, tc := range map[string]hopCase{
		"a hop that ends before its relay would start": {hopSeconds: "0.1", hopExit: "0", wantRelays: -1},
		"a switch over the forwarded socket":           {hopSeconds: "0.1", hopExit: "0", farShell: "thirdbox:notes", wantFollow: next, wantRelays: -1},
		"a switch that stands when the shell exits 1":  {hopSeconds: "0.1", hopExit: "1", farShell: "thirdbox:notes", wantFollow: next, wantRelays: -1},
		"a hop whose connection failed":                {hopSeconds: "0.1", hopExit: "255", wantErr: true, wantRelays: -1},
		"a switch lost with the connection":            {hopSeconds: "0.1", hopExit: "255", farShell: "thirdbox:notes", wantErr: true, wantRelays: -1},
		"a relay that stays silent":                    {hopSeconds: "2.5", hopExit: "0", wantRelays: 1},
		// Long enough that a relay wrongly started again would show.
		"a relay that steps aside while the hop goes on":              {hopSeconds: "4.5", hopExit: "0", relay: "aside", wantRelays: 1},
		"a far camp that predates relays":                             {hopSeconds: "4.5", hopExit: "0", relay: "predates", wantRelays: 1},
		"a switch over the relay":                                     {hopSeconds: "0.1", hopExit: "0", farShell: "thirdbox:notes", farRoute: "relay", farDelay: "2.5", relay: "real", wantFollow: next, wantRelays: 1},
		"a switch over a relay started again after its link was lost": {hopSeconds: "0.1", hopExit: "0", farShell: "thirdbox:notes", farRoute: "relay", farDelay: "4.5", relay: "lost-once", wantFollow: next, wantRelays: 2},
		"a switch over a relay started again after its ssh failed":    {hopSeconds: "0.1", hopExit: "0", farShell: "thirdbox:notes", farRoute: "relay", farDelay: "4.5", relay: "unreachable-once", wantFollow: next, wantRelays: 2},
		// One start and hopRelayRestarts more, then no further attempts.
		"a relay whose ssh keeps failing is given up on": {hopSeconds: "10", hopExit: "0", relay: "unreachable", wantRelays: 1 + hopRelayRestarts},
		"a hop cancelled with its relay running":         {hopSeconds: "31.7", hopExit: "0", cancelAfter: 2500 * time.Millisecond, wantErr: true, wantRelays: 1},
	} {
		t.Run(name, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "ssh.log")
			t.Setenv("FAKE_SSH_LOG", log)
			t.Setenv("FAKE_SSH_HOP_SECONDS", tc.hopSeconds)
			t.Setenv("FAKE_SSH_HOP_EXIT", tc.hopExit)
			t.Setenv("CAMP_TEST_FAR_SHELL", tc.farShell)
			t.Setenv("FAKE_SSH_FAR_ROUTE", tc.farRoute)
			t.Setenv("FAKE_SSH_FAR_DELAY", cmp.Or(tc.farDelay, "0"))
			t.Setenv("FAKE_SSH_RELAY", tc.relay)
			// Other hops may be open on this machine; only what this one
			// leaves counts.
			before, _ := filepath.Glob("/tmp/camp-hr-*")
			hopTime, _ := time.ParseDuration(tc.hopSeconds + "s")
			farDelay, _ := time.ParseDuration(cmp.Or(tc.farDelay, "0") + "s")
			expected := hopTime + farDelay
			ctx := context.Background()
			if tc.cancelAfter > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.cancelAfter)
				defer cancel()
				expected = tc.cancelAfter
			}

			started := time.Now()
			follow, err := runHop(ctx, line, around)
			held := time.Since(started)

			if (err != nil) != tc.wantErr || follow != tc.wantFollow {
				far, _ := os.ReadFile(log + ".far")
				t.Fatalf("follow-up %q err %v, want %q err=%v\n%s", follow, err, tc.wantFollow, tc.wantErr, far)
			}
			// Nothing but the hop itself may hold the opening shell.
			if held > expected+3*time.Second {
				t.Fatalf("the opening shell was held %v for a hop of %v", held, expected)
			}
			calls, _ := os.ReadFile(log)
			if strings.Contains(string(calls), "far-shell-failed") {
				far, _ := os.ReadFile(log + ".far")
				t.Fatalf("the far shell's switch failed:\n%s", far)
			}
			relays := strings.Count(string(calls), "relay\n")
			switch {
			case tc.wantRelays >= 0 && relays != tc.wantRelays:
				t.Fatalf("relay started %d times, want %d (calls: %q)", relays, tc.wantRelays, calls)
			case tc.wantRelays < 0 && relays != 0 && held < hopRelayStartDelay:
				t.Fatalf("the relay started ahead of its delay (hop held %v; calls: %q)", held, calls)
			}
			if relays > 0 {
				argv, _ := os.ReadFile(log + ".relay-argv")
				if !strings.HasPrefix(string(argv), "-T\n") || !strings.Contains(string(argv), "far.example\n") || !strings.Contains(string(argv), "hop-resume relay ") {
					t.Fatalf("relay ssh call: %q", argv)
				}
				// What the far side prints on the relay's stderr is its to
				// choose, and must not reach this terminal.
				if stderr, _ := os.ReadFile(log + ".relay-stderr"); string(stderr) != os.DevNull+"\n" {
					t.Fatalf("the relay's ssh was given %q for its stderr, want %s", stderr, os.DevNull)
				}
				pid, _ := os.ReadFile(log + ".relay-pid")
				var relayPID int
				if _, err := fmt.Sscan(string(pid), &relayPID); err != nil {
					t.Fatalf("relay pid %q: %v", pid, err)
				}
				gone := false
				for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
					if syscall.Kill(relayPID, 0) != nil {
						gone = true
						break
					}
				}
				if !gone {
					t.Fatal("the relay's ssh outlived the hop")
				}
			}
			// The hop's own wait is the last thing it could leave running.
			waiting := true
			for deadline := time.Now().Add(2 * time.Second); waiting && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				waiting = sleeping(tc.hopSeconds)
			}
			if waiting {
				t.Fatalf("the hop's ssh left its wait of %ss running", tc.hopSeconds)
			}
			after, _ := filepath.Glob("/tmp/camp-hr-*")
			if left := slices.DeleteFunc(after, func(path string) bool { return slices.Contains(before, path) }); len(left) != 0 {
				t.Fatalf("the hop left sockets behind: %v", left)
			}
		})
	}
}
