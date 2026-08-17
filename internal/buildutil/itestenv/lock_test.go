package itestenv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mustCreate makes an empty file (and its parents) inside a test's temp dir.
func mustCreate(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%s): %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close(%s): %v", path, err)
	}
}

// The lock keys on the daemon, so suites aimed at one VM serialize while
// suites on different VMs never wait on each other.
func TestSuiteLockPathKeysOnTheDaemon(t *testing.T) {
	t.Parallel()

	home := "/home/u"
	dedicated := SuiteLockPath(home, ProfileDockerHost(home, ProfileName))

	tests := []struct {
		name       string
		dockerHost string
		wantSame   bool
	}{
		{name: "same socket, same lock", dockerHost: ProfileDockerHost(home, ProfileName), wantSame: true},
		{name: "same socket spelled loosely, same lock", dockerHost: "unix://" + home + "/.colima//camp-itest/docker.sock", wantSame: true},
		{name: "default profile gets its own lock", dockerHost: ProfileDockerHost(home, "default")},
		{name: "remote daemon gets its own lock", dockerHost: "tcp://remote:2375"},
		{name: "unset docker host still keys deterministically", dockerHost: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SuiteLockPath(home, tt.dockerHost)
			if dir := filepath.Dir(got); dir != filepath.Join(home, obeyDirName, locksDirName) {
				t.Errorf("lock directory = %q, want %q", dir, filepath.Join(home, obeyDirName, locksDirName))
			}
			if !strings.HasSuffix(got, lockNameSuffix) {
				t.Errorf("lock path = %q, want a %s suffix", got, lockNameSuffix)
			}
			if same := got == dedicated; same != tt.wantSame {
				t.Errorf("SuiteLockPath(%q) == dedicated is %v, want %v (%q vs %q)",
					tt.dockerHost, same, tt.wantSame, got, dedicated)
			}
			if again := SuiteLockPath(home, tt.dockerHost); again != got {
				t.Errorf("SuiteLockPath is not deterministic: %q then %q", got, again)
			}
		})
	}
}

// A cancelled context must be answered before any filesystem work, so an
// interrupted run leaves no lock artifacts behind.
func TestAcquireHonoursCancellationBeforeTouchingDisk(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "locks", "camp-itest.lock")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	lock, err := Acquire(ctx, path, LockOptions{})
	if err == nil {
		_ = lock.Release()
		t.Fatal("Acquire() with a cancelled context returned no error")
	}
	if _, statErr := os.Stat(filepath.Dir(path)); statErr == nil {
		t.Errorf("Acquire() created %s despite the cancelled context", filepath.Dir(path))
	}
}

func TestLockWait(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "unset uses the default", want: DefaultLockWait},
		{name: "parsed duration wins", value: "90s", want: 90 * time.Second},
		{name: "garbage falls back", value: "soon", want: DefaultLockWait},
		{name: "zero falls back", value: "0s", want: DefaultLockWait},
		{name: "negative falls back", value: "-5m", want: DefaultLockWait},
		{name: "whitespace is trimmed", value: "  2m  ", want: 2 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := LockWait(func(string) string { return tt.value })
			if got != tt.want {
				t.Errorf("LockWait(%q) = %s, want %s", tt.value, got, tt.want)
			}
		})
	}
}

// The waiting notice has one job: tell the reader which process to go and look
// at. A record it cannot parse must degrade to an honest phrase, never to a
// fabricated pid.
func TestHolderRecordRoundTrip(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 8, 16, 14, 5, 6, 0, time.UTC)

	tests := []struct {
		name     string
		data     string
		wantOK   bool
		contains string
	}{
		{
			name:     "a well formed record names the pid",
			data:     `{"pid":4711,"label":"integration suite","started":"` + started.Format(time.RFC3339) + `"}`,
			wantOK:   true,
			contains: "pid 4711",
		},
		{name: "empty file", contains: "another process"},
		{name: "truncated json", data: `{"pid":47`, contains: "another process"},
		{name: "missing pid", data: `{"label":"x"}`, contains: "another process"},
		{name: "zero pid is not a holder", data: `{"pid":0}`, contains: "another process"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, ok := parseHolder([]byte(tt.data))
			if ok != tt.wantOK {
				t.Errorf("parseHolder(%q) ok = %v, want %v", tt.data, ok, tt.wantOK)
			}
			if got := describeHolder(h, ok); !strings.Contains(got, tt.contains) {
				t.Errorf("describeHolder() = %q, want it to contain %q", got, tt.contains)
			}
			notice := waitingNotice("integration suite", 42*time.Second, h, ok)
			if !strings.Contains(notice, "waiting for integration suite") {
				t.Errorf("waitingNotice() = %q, want it to name what it waits for", notice)
			}
			if !strings.Contains(notice, "42s") {
				t.Errorf("waitingNotice() = %q, want it to report the elapsed wait", notice)
			}
		})
	}
}

func TestProfileLockPath(t *testing.T) {
	t.Parallel()

	if _, err := profileLockPath("/home/u", ""); err == nil {
		t.Fatal("profileLockPath() accepted an empty profile name")
	}
	got, err := profileLockPath("/home/u", ProfileName)
	if err != nil {
		t.Fatalf("profileLockPath() error = %v", err)
	}
	want := filepath.Join("/home/u", obeyDirName, locksDirName, lockNamePrefix+"profile-"+ProfileName+lockNameSuffix)
	if got != want {
		t.Fatalf("profileLockPath() = %q, want %q", got, want)
	}
}

// The suite lock and the profile start lock must never be the same file: the
// start lock is held while a VM boots, inside the window a run holds the suite
// lock.
func TestProfileLockIsDistinctFromSuiteLock(t *testing.T) {
	t.Parallel()

	home := "/home/u"
	start, err := profileLockPath(home, ProfileName)
	if err != nil {
		t.Fatalf("profileLockPath() error = %v", err)
	}
	if suite := SuiteLockPath(home, ProfileDockerHost(home, ProfileName)); suite == start {
		t.Fatalf("suite lock and profile start lock share %q", suite)
	}
}
