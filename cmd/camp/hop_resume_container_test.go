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
		follow, _ := serveHopResume(context.Background(), ln, "", new(hopSwitch), func(_ context.Context, request hopResumeRequest) (string, error) {
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

func TestHopResumeSendFallsBackToLoopbackWithToken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	const token = "0123456789abcdef0123456789abcdef"
	tokenFile := filepath.Join(t.TempDir(), "camp-ht-test")
	if err := os.WriteFile(tokenFile, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	// The forwarded socket an ssh server bound as another user: named in the
	// environment, closed to this shell.
	t.Setenv(hopResumeSockEnv, filepath.Join(t.TempDir(), "camp-hr-r.sock"))
	t.Setenv(hopResumeAddrEnv, ln.Addr().String())
	t.Setenv(hopResumeTokenFileEnv, tokenFile)
	reject := true
	served := make(chan error, 1)
	go func() {
		_, err := serveHopResume(context.Background(), ln, token, new(hopSwitch), func(_ context.Context, request hopResumeRequest) (string, error) {
			if request.Selector != "thirdbox:notes" || request.Host != "c.example" {
				return "", errors.New("request changed in transit")
			}
			if reject {
				reject = false
				return "", errors.New("parent route unavailable")
			}
			return "ssh prepared-parent-route", nil
		})
		served <- err
	}()

	err = sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example")
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("rejected route: %v", err)
	}
	if _, statErr := os.Stat(tokenFile); statErr != nil {
		t.Fatalf("a rejected request must leave the token for the next attempt: %v", statErr)
	}
	if err := sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example"); err != nil {
		t.Fatalf("send over loopback: %v", err)
	}
	if err := <-served; err != nil {
		t.Fatalf("parent: %v", err)
	}
	if _, statErr := os.Stat(tokenFile); !os.IsNotExist(statErr) {
		t.Fatalf("an accepted request must remove the token file: %v", statErr)
	}

	err = sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example")
	if err == nil || !strings.Contains(err.Error(), "camp-hr-r.sock") {
		t.Fatalf("without a token the loopback route must stay unused and the socket failure surface: %v", err)
	}
}

// The reviewer's scenario end to end: a shell that outlived its hop still has
// the address and the token file, and someone else now holds the port.
func TestHopResumeSendRefusesAListenerWithoutTheToken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	const token = "0123456789abcdef0123456789abcdef"
	tokenFile := filepath.Join(t.TempDir(), "camp-ht-test")
	if err := os.WriteFile(tokenFile, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(hopResumeSockEnv, filepath.Join(t.TempDir(), "camp-hr-r.sock"))
	t.Setenv(hopResumeAddrEnv, ln.Addr().String())
	t.Setenv(hopResumeTokenFileEnv, tokenFile)
	received := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		// Acknowledge at once, as a listener that wants the shell gone would.
		_, _ = io.WriteString(conn, `{"version":2,"accepted":true}`+"\n"+`{"version":1,"accepted":true}`+"\n")
		all, _ := io.ReadAll(conn)
		received <- string(all)
	}()

	err = sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example")
	if err == nil || !strings.Contains(err.Error(), "not the shell that opened this hop") {
		t.Fatalf("a forged acknowledgement must not end the shell: %v", err)
	}
	if _, statErr := os.Stat(tokenFile); statErr != nil {
		t.Fatalf("the token file must survive a forged acknowledgement: %v", statErr)
	}
	if got := <-received; strings.Contains(got, token) || strings.Contains(got, "thirdbox") {
		t.Fatalf("the far shell disclosed its token or request to an unproven listener: %s", got)
	}
}

func TestHopResumeTokenScriptKeepsTokenPrivate(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	home := t.TempDir()
	dir := filepath.Join(home, ".obey", "hop-resume")
	// A directory left open to others, a token from a hop that died long ago,
	// and one from a hop that may still be alive.
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	stale, live := filepath.Join(dir, "ht-stale1"), filepath.Join(dir, "ht-alive1")
	for _, path := range []string{stale, live} {
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	longAgo := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(stale, longAgo, longAgo); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("/bin/sh", "-c", hopTokenScript)
	cmd.Env = append(os.Environ(), "HOME="+home, "TMPDIR="+t.TempDir())
	cmd.Stdin = strings.NewReader(token)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("token script: %v", err)
	}
	path := string(out)
	if filepath.Dir(path) != dir {
		t.Fatalf("the token must be stored under the user's own %s, got %q", dir, path)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("token directory must be closed to other users: %v %v", info.Mode().Perm(), err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode %v, want 0600", info.Mode().Perm())
	}
	if got := readHopToken(path); got != token {
		t.Fatalf("the far shell must accept the file the script wrote: %q", got)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("a month-old token must be pruned: %v", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("a recent token may belong to a live hop and must stay: %v", err)
	}
}

// A shell that outlives its hop keeps the token path. Wherever another user
// could have written that path, its contents are theirs to choose, and with
// them a valid proof.
func TestHopResumeRefusesATokenFileOthersCouldHaveWritten(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	private := t.TempDir()
	shared := t.TempDir()
	if err := os.Chmod(shared, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	write := func(dir, name string, mode os.FileMode) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(token), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	trusted := write(private, "ht-own", 0o600)
	link := filepath.Join(private, "ht-link")
	if err := os.Symlink(trusted, link); err != nil {
		t.Fatal(err)
	}
	if got := readHopToken(trusted); got != token {
		t.Fatalf("a private file in a private directory is the hop's token: %q", got)
	}
	// The suite runs as root, which can hand a file to another user.
	const stranger = 54321
	foreign := write(private, "ht-foreign", 0o600)
	if err := os.Chown(foreign, stranger, stranger); err != nil {
		t.Fatal(err)
	}
	foreignDir := t.TempDir()
	inForeignDir := write(foreignDir, "ht-own", 0o600)
	if err := os.Chown(foreignDir, stranger, stranger); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"owned by another user":              foreign,
		"in another user's directory":        inForeignDir,
		"recreated in a shared directory":    write(shared, "ht-recreated", 0o644),
		"private file in a shared directory": write(shared, "ht-private", 0o600),
		"readable by others":                 write(private, "ht-open", 0o644),
		"a symlink":                          link,
		"missing":                            filepath.Join(private, "ht-gone"),
	} {
		if got := readHopToken(path); got != "" {
			t.Errorf("%s: token file must not be trusted, got %q", name, got)
		}
	}

	// End to end: the stranger holds the port and knows the token in the file
	// they recreated, so their proof would verify.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	reached := make(chan struct{}, 1)
	go func() {
		_, _ = serveHopResume(context.Background(), ln, token, new(hopSwitch), func(context.Context, hopResumeRequest) (string, error) {
			reached <- struct{}{}
			return "ssh strangers-route", nil
		})
	}()
	t.Setenv(hopResumeSockEnv, filepath.Join(private, "camp-hr-r.sock"))
	t.Setenv(hopResumeAddrEnv, ln.Addr().String())
	t.Setenv(hopResumeTokenFileEnv, filepath.Join(shared, "ht-recreated"))
	err = sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example")
	if err == nil || !strings.Contains(err.Error(), "camp-hr-r.sock") {
		t.Fatalf("with an untrusted token file the loopback route must not be used at all: %v", err)
	}
	select {
	case <-reached:
		t.Fatal("the far shell proved itself to a stranger's listener")
	default:
	}
}

// The forwarded socket lives in a shared directory. After the hop ends and the
// ssh server removes it, anyone can put a socket of their own at that path.
func TestHopResumeRefusesASocketAnotherUserOwns(t *testing.T) {
	shared := t.TempDir()
	if err := os.Chmod(shared, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(shared, "camp-hr-r.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	if err := os.Chmod(sock, 0o666); err != nil {
		t.Fatal(err)
	}
	reached := make(chan struct{}, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		reached <- struct{}{}
		// Acknowledge at once, as a listener that wants the shell gone would.
		_, _ = io.WriteString(conn, `{"version":1,"accepted":true}`+"\n")
		_ = conn.Close()
	}()
	t.Setenv(hopResumeSockEnv, sock)
	t.Setenv(hopResumeAddrEnv, "")
	t.Setenv(hopResumeTokenFileEnv, "")

	// As the ssh server leaves it, owned by this user, the socket is the route.
	if err := trustedHopSocket(sock); err != nil {
		t.Fatalf("a socket this user owns must be trusted: %v", err)
	}
	// The suite runs as root, which can hand the socket to another user.
	if err := os.Chown(sock, 54321, 54321); err != nil {
		t.Fatal(err)
	}
	err = sendHopResume(context.Background(), "thirdbox:notes", cmdutil.CampaignScope{}, "c.example")
	if err == nil || !strings.Contains(err.Error(), "does not belong to the shell that opened this hop") {
		t.Fatalf("a socket another user owns must not be dialed: %v", err)
	}
	select {
	case <-reached:
		t.Fatal("the far shell connected to another user's socket")
	case <-time.After(100 * time.Millisecond):
	}

	file := filepath.Join(shared, "camp-hr-file.sock")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := trustedHopSocket(file); err == nil {
		t.Fatal("a path that is not a socket must not be trusted")
	}
}
