package transfer

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/machines"
)

func testMachine() *machines.Machine {
	return &machines.Machine{
		ID:         "archdtop",
		Host:       "archdtop.tail37114b.ts.net",
		SSHUser:    "lance",
		AuthMethod: machines.AuthTailscaleSSH,
	}
}

func TestRsyncArgsShape(t *testing.T) {
	o := CopyOptions{Machine: testMachine(), Force: false}
	args := strings.Join(o.rsyncArgs("src", "dest"), " ")

	for _, want := range []string{"-a", "-s", "--no-links", "--timeout=60", "--ignore-existing", "-e "} {
		if !strings.Contains(args, want) {
			t.Errorf("rsync args missing %q: %s", want, args)
		}
	}
	// Copy-only is structural: no argv in any path may carry a delete flag.
	for _, forbidden := range []string{"--delete", "--remove-source-files"} {
		if strings.Contains(args, forbidden) {
			t.Errorf("rsync args carry %q, which would break the copy-only invariant: %s", forbidden, args)
		}
	}
	// The ssh options must come from the shared builder, not a second copy.
	if !strings.Contains(args, "ControlMaster=auto") || !strings.Contains(args, "BatchMode=yes") {
		t.Errorf("rsync -e value is not the shared hop option set: %s", args)
	}
}

func TestRsyncForceDropsIgnoreExisting(t *testing.T) {
	o := CopyOptions{Machine: testMachine(), Force: true}
	if strings.Contains(strings.Join(o.rsyncArgs("s", "d"), " "), "--ignore-existing") {
		t.Error("--force must allow the overwrite")
	}
}

func TestScpArgsQuoteTheRemotePath(t *testing.T) {
	o := CopyOptions{Machine: testMachine()}
	args := o.scpArgs("lance@host:/srv/a b/c.md", "/local/x.md")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, `'/srv/a b/c.md'`) {
		t.Errorf("remote path must be quoted for the remote shell: %s", joined)
	}
	if !strings.Contains(joined, "ServerAliveInterval=15") {
		t.Errorf("scp needs keepalives, since it has no --timeout: %s", joined)
	}
	if strings.Contains(joined, "'/local/x.md'") {
		t.Errorf("the local path must not be shell-quoted; it never reaches a shell: %s", joined)
	}
}

func TestRemoteLacksRsyncSignature(t *testing.T) {
	exit12 := runExit(t, 12)
	exit1 := runExit(t, 1)

	tests := []struct {
		name string
		err  error
		out  string
		want bool
	}{
		{"exit 12 with connection closed", exit12, "rsync: connection unexpectedly closed", true},
		{"exit 12 with command not found", exit12, "bash: rsync: command not found", true},
		// Exit 12 is a general protocol error; without the signature it is a
		// real failure, and retrying with scp would hide it.
		{"exit 12 alone is not the signature", exit12, "some other protocol error", false},
		{"a different exit code is not the signature", exit1, "connection unexpectedly closed", false},
		{"a non-exec error is not the signature", errors.New("boom"), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := remoteLacksRsync(tt.err, []byte(tt.out)); got != tt.want {
				t.Errorf("remoteLacksRsync = %v, want %v", got, tt.want)
			}
		})
	}
}

// runExit produces a real *exec.ExitError with the given code.
func runExit(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit "+itoa(code)).Run()
	if err == nil {
		t.Fatalf("expected a non-zero exit")
	}
	return err
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestCopyRemoteFallsBackToScpOnlyOnTheSignature(t *testing.T) {
	// Both fleet machines have rsync, so the fallback can never be exercised
	// naturally. Forcing it here is the only way it ships executed.
	var calls []string
	opts := CopyOptions{
		Machine:  testMachine(),
		Endpoint: Endpoint{Machine: "archdtop", Campaign: "c", Path: "x.md"},
		Local:    "/local/x.md",
		Pull:     true,
		LookPath: func(string) (string, error) { return "/usr/bin/found", nil },
		Run: func(_ context.Context, name string, _ ...string) ([]byte, error) {
			calls = append(calls, name)
			if name == "rsync" {
				return []byte("rsync: connection unexpectedly closed"), runExit(t, 12)
			}
			return nil, nil
		},
	}
	// resolveRoot would need ssh, so drive the copy step directly.
	if _, runErr := opts.Run(context.Background(), "rsync"); runErr == nil {
		t.Fatal("fixture should fail")
	}
	calls = nil

	if err := copyWithFallback(context.Background(), opts, "remote:src", "/local/x.md"); err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	if strings.Join(calls, ",") != "rsync,scp" {
		t.Errorf("call sequence = %v, want rsync then scp", calls)
	}
}

func TestCopyRemoteDoesNotFallBackOnARealFailure(t *testing.T) {
	var calls []string
	opts := CopyOptions{
		Machine:  testMachine(),
		Pull:     true,
		LookPath: func(string) (string, error) { return "/usr/bin/found", nil },
		Run: func(_ context.Context, name string, _ ...string) ([]byte, error) {
			calls = append(calls, name)
			return []byte("permission denied"), runExit(t, 23)
		},
	}
	err := copyWithFallback(context.Background(), opts, "remote:src", "/local/x.md")
	if err == nil {
		t.Fatal("a real failure must surface, not silently retry")
	}
	if strings.Join(calls, ",") != "rsync" {
		t.Errorf("scp must not run for a non-signature failure, calls = %v", calls)
	}
	if !strings.Contains(err.Error(), "pull from archdtop") {
		t.Errorf("error must name direction and machine: %v", err)
	}
}

func TestCopyRemoteMissingBothBinaries(t *testing.T) {
	opts := CopyOptions{
		Machine:  testMachine(),
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Run:      func(context.Context, string, ...string) ([]byte, error) { return nil, nil },
	}
	err := copyWithFallback(context.Background(), opts, "s", "d")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"rsync not found on archdtop", "scp not found locally"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must name which binary is missing where: %v", err)
		}
	}
}
