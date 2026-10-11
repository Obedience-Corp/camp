//go:build container_fs

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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
	go func() { _, _ = io.Copy(io.Discard, relay.fromFar) }()
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
	abandon := func(name string) string {
		path := filepath.Join(dir, name)
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		ln.(*net.UnixListener).SetUnlinkOnClose(false)
		_ = ln.Close()
		return path
	}
	// A relay killed long ago, one whose socket is seconds old and may be
	// about to listen, and one still serving.
	dead := abandon("aaaaaaaaaaaaaaaa.sock")
	longAgo := time.Now().Add(-time.Hour)
	if err := os.Chtimes(dead, longAgo, longAgo); err != nil {
		t.Fatal(err)
	}
	starting := abandon("cccccccccccccccc.sock")
	live := filepath.Join(dir, "bbbbbbbbbbbbbbbb.sock")
	serving, err := net.Listen("unix", live)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = serving.Close() }()
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

	relay := startTestHopRelay(t, "0123456789abcdef")
	go func() { _, _ = io.Copy(io.Discard, relay.fromFar) }()
	if _, err := os.Lstat(dead); !os.IsNotExist(err) {
		t.Fatalf("an old socket nothing listens on must be pruned: %v", err)
	}
	if _, err := os.Lstat(starting); err != nil {
		t.Fatalf("a socket created moments ago may belong to a relay still starting: %v", err)
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
	killed, err := net.Listen("unix", filepath.Join(dir, id+".sock"))
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
