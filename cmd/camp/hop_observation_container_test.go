//go:build container_fs && !windows

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// The native SSH journey proves the transport/UI. This regression pins the
// ownership boundary: B returns a selector without a binding, then A opens C
// with its own binding and a new observation scope.
func TestHopResumeFSOnwardAppFollowUsesOpeningCoordinator(t *testing.T) {
	ctx := context.Background()
	t.Setenv("CAMP_MACHINES_PATH", filepath.Join(t.TempDir(), "machines.yaml"))
	c := machines.Machine{ID: "local-c", Host: "c.example", AuthMethod: machines.AuthSSHAgent}
	registry := &machines.File{Machines: []machines.Machine{c}}
	if err := registry.Save(); err != nil {
		t.Fatal(err)
	}

	// B has no App binding. Its onward switch must unwind, not open another SSH.
	t.Setenv("FESTIVAL_APP_TERMINAL_BINDING", "")
	t.Setenv("SSH_CONNECTION", "a.example 1234 b.example 22")
	t.Setenv(HopOriginEnvVar, "v1;host=a.example;user=fixture")
	t.Setenv(hopResumeSockEnv, "/fixture/private.sock")
	selector, err := cmdutil.ParseMachineSelector("local-c:notes")
	if err != nil {
		t.Fatal(err)
	}
	cmd, stdout, _ := newHopBackCmd()
	if err := dispatchRemoteSwitch(ctx, cmd, selector, false, true, false); err != nil {
		t.Fatal(err)
	}
	if line := stdout.String(); !strings.Contains(line, "hop-resume send") || !strings.Contains(line, "--host 'c.example'") || !strings.HasSuffix(line, "&& exit\n") {
		t.Fatalf("B must send the onward selector and exit: %q", line)
	}

	// The real App command is represented by an argv/binding recorder here.
	// Both follow lifecycles execute on A, whose local binding never left.
	clearSSHSessionEnv(t)
	t.Setenv(HopOriginEnvVar, "")
	t.Setenv("FESTIVAL_APP_TERMINAL_BINDING", "local-binding")
	bin := t.TempDir()
	log := filepath.Join(bin, "app.log")
	t.Setenv("CAMP_TEST_APP_LOG", log)
	if err := os.WriteFile(filepath.Join(bin, "camp-app"), []byte("#!/bin/sh\nprintf '%s|%s\\n' \"$FESTIVAL_APP_TERMINAL_BINDING\" \"$*\" >> \"$CAMP_TEST_APP_LOG\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	b := hopLine{remoteCmd: "export 'CAMP_HOP_MACHINE_ID=b' && exec $SHELL -l"}
	const firstID, secondID = "1111111111111111", "2222222222222222"
	prepare := func(_ context.Context, r hopResumeRequest) (string, error) {
		mapped, err := mapHopResume(r, registry)
		if err != nil {
			return "", err
		}
		if mapped.Selector != "local-c:notes" {
			t.Fatalf("wrong opening-side route: %+v", mapped)
		}
		var line bytes.Buffer
		err = emitShellConnect(&line, true, "/notes", remote.Direct(&c), "")
		return line.String(), err
	}
	first, endFirst := followHop(ctx, &b, firstID, io.Discard, prepare)
	if _, err := first(ctx, hopResumeRequest{Observation: &hopObservation{firstID, 1, "b-camp", "/b", "/b"}}); err != nil {
		t.Fatal(err)
	}
	line, err := first(ctx, hopResumeRequest{Selector: "b-alias:notes", Host: "c.example"})
	endFirst()
	if err != nil {
		t.Fatal(err)
	}
	next, err := splitHopLine(line)
	if err != nil || next.machineID() != c.ID {
		t.Fatalf("onward command lost local machine metadata: %q (%v)", line, err)
	}
	second, endSecond := followHop(ctx, &next, secondID, io.Discard, prepare)
	defer endSecond()
	if !strings.Contains(next.remoteCmd, hopFollowEnv+"='"+secondID+"'") {
		t.Fatalf("C did not receive a fresh scope: %s", next.remoteCmd)
	}
	if _, err := second(ctx, hopResumeRequest{Observation: &hopObservation{firstID, 2, "b-camp", "/b", "/b"}}); err == nil {
		t.Fatal("C's coordinator accepted a late report from B")
	}
	if _, err := second(ctx, hopResumeRequest{Observation: &hopObservation{secondID, 1, "c-camp", "/notes", "/notes/projects"}}); err != nil {
		t.Fatalf("C's report did not reach the original App: %v", err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	calls := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(calls) != 5 {
		t.Fatalf("expected B start/entered/end then C start/entered: %s", raw)
	}
	for _, call := range calls {
		if !strings.HasPrefix(call, "local-binding|") {
			t.Fatalf("App call lost opening-side binding: %s", call)
		}
	}
	if !strings.Contains(calls[3], "--machine-id local-c") || !strings.Contains(calls[4], "--camp-id c-camp") {
		t.Fatalf("App did not follow C: %s", raw)
	}
}

func TestHopResumeFSAppTerminalStampsOriginWithoutLosingFollowScope(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FESTIVAL_APP_TERMINAL_BINDING", "local-binding")
	log := filepath.Join(bin, "argv.log")
	t.Setenv("CAMP_TEST_ARGV_LOG", log)
	for name, script := range map[string]string{
		"ssh":      "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAMP_TEST_ARGV_LOG\"\n",
		"camp-app": "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	origin := "v1;host=opening.example;user=fixture"
	around := hopSurroundings{
		origin:  origin,
		prepare: prepareHopResume,
		terminal: func() (*os.File, *os.File, bool, func(), error) {
			f, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
			return f, f, true, func() { _ = f.Close() }, err
		},
	}
	const line = "ssh -t 'remote.example' 'export '\\''CAMP_HOP_MACHINE_ID=remote'\\'' && exec $SHELL -l'"
	if _, err := runHop(context.Background(), line, around); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"export " + HopOriginEnvVar + "='" + origin + "'",
		"export " + hopFollowEnv + "=",
		"CAMP_HOP_MACHINE_ID=remote",
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("App terminal SSH command lost %q: %s", want, raw)
		}
	}
}
