package itestenv

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// fakeColima stands in for the Colima CLI so the resolution decision table can
// be exercised on any machine, including one with no Colima and no VM to boot.
type fakeColima struct {
	status      ProfileStatus
	statusErr   error
	afterStart  ProfileStatus
	startErr    error
	starts      []StartSpec
	statusCalls int
	// later, when set, is the answer from the second Status call onwards: what
	// the profile looks like after this entrant waited for the start lock,
	// which is when a racing entrant gets to change it.
	later    *ProfileStatus
	laterErr error
}

func (f *fakeColima) Status(_ context.Context, profile string) (ProfileStatus, error) {
	f.statusCalls++
	if f.statusCalls > 1 {
		if f.laterErr != nil {
			return ProfileStatus{Name: profile}, f.laterErr
		}
		if f.later != nil {
			return *f.later, nil
		}
	}
	if f.statusErr != nil {
		return ProfileStatus{Name: profile}, f.statusErr
	}
	if len(f.starts) > 0 {
		return f.afterStart, nil
	}
	return f.status, nil
}

func (f *fakeColima) Start(_ context.Context, spec StartSpec, _ io.Writer) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.starts = append(f.starts, spec)
	return nil
}

func env(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func TestResolveDecisions(t *testing.T) {
	t.Parallel()

	running := ProfileStatus{Name: ProfileName, Exists: true, Running: true, Status: "Running"}
	stopped := ProfileStatus{Name: ProfileName, Exists: true, Status: "Stopped"}
	absent := ProfileStatus{Name: ProfileName}

	tests := []struct {
		name       string
		vars       map[string]string
		colima     *fakeColima
		autoStart  bool
		wantSource Source
		wantHost   string // "" means "the dedicated profile socket"
		wantStart  bool
		wantReason string
		wantErr    string
	}{
		{
			name:       "explicit override wins over everything",
			vars:       map[string]string{EnvDockerHost: "tcp://remote:2375"},
			colima:     &fakeColima{status: running},
			wantSource: SourceOverride,
			wantHost:   "tcp://remote:2375",
		},
		{
			name:       "isolation disabled falls back to the ambient daemon",
			vars:       map[string]string{EnvProfile: ProfileDisabled, DockerHostVar: "unix:///shared/docker.sock"},
			colima:     &fakeColima{status: running},
			wantSource: SourceFallback,
			wantHost:   "unix:///shared/docker.sock",
			wantReason: EnvProfile + "=" + ProfileDisabled,
		},
		{
			name: "a non-Colima DOCKER_HOST is somebody's decision, not ours",
			vars: map[string]string{DockerHostVar: "tcp://remote:2375"},
			// A remote daemon must not be redirected onto a local profile, and
			// must not cause a VM to be created behind the operator's back.
			colima:     &fakeColima{status: absent},
			autoStart:  true,
			wantSource: SourceOverride,
			wantHost:   "tcp://remote:2375",
		},
		{
			name:       "the shared Colima profile is ours to redirect",
			vars:       map[string]string{DockerHostVar: "unix:///HOME/.colima/default/docker.sock"},
			colima:     &fakeColima{status: running},
			autoStart:  true,
			wantSource: SourceProfile,
		},
		{
			name:       "running profile is used without starting anything",
			colima:     &fakeColima{status: running},
			autoStart:  true,
			wantSource: SourceProfile,
		},
		{
			name:       "stopped profile starts on demand",
			colima:     &fakeColima{status: stopped, afterStart: running},
			autoStart:  true,
			wantSource: SourceProfile,
			wantStart:  true,
		},
		{
			name:       "absent profile is created on demand",
			colima:     &fakeColima{status: absent, afterStart: running},
			autoStart:  true,
			wantSource: SourceProfile,
			wantStart:  true,
		},
		{
			name:       "stopped profile without autostart reports how to fix it",
			colima:     &fakeColima{status: stopped},
			wantSource: SourceFallback,
			wantReason: "is stopped",
		},
		{
			// Sharing a daemon after failing to get a dedicated one re-creates
			// the collapse this package exists to remove, so the run refuses
			// instead of running somewhere it was not asked to run.
			name:      "a failed start refuses the run rather than sharing a daemon",
			colima:    &fakeColima{status: absent, startErr: io.ErrClosedPipe},
			autoStart: true,
			wantErr:   "could not start the dedicated integration daemon",
		},
		{
			// A machine with no Colima has no dedicated daemon to be given, so
			// sharing one loudly is the only thing left that runs the suite.
			name:       "no colima still falls back loudly",
			colima:     &fakeColima{statusErr: io.ErrUnexpectedEOF},
			autoStart:  true,
			wantSource: SourceFallback,
			wantReason: "Colima is unavailable",
		},
		{
			// Reading the profile's state after the lock is what makes the
			// sizing decision safe; if that read fails, both sizing answers are
			// wrong in a way somebody has to undo.
			name:      "an unreadable state after the lock refuses rather than guesses",
			colima:    &fakeColima{status: absent, laterErr: io.ErrUnexpectedEOF},
			autoStart: true,
			wantErr:   "confirm the state of profile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			vars := make(map[string]string, len(tt.vars))
			for key, value := range tt.vars {
				// "/HOME/..." stands in for the per-test home directory, which
				// only exists once the test is running.
				vars[key] = strings.Replace(value, "/HOME", home, 1)
			}
			got, err := Resolve(context.Background(), Options{
				Getenv:    env(vars),
				Home:      home,
				Colima:    tt.colima,
				AutoStart: tt.autoStart,
				Out:       io.Discard,
			})
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Resolve() = %+v, want an error mentioning %q", got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Resolve() error = %v, want it to mention %q", err, tt.wantErr)
				}
				if got.Source != "" {
					t.Errorf("Resolve() returned %+v alongside its error, want the zero resolution", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got.Source != tt.wantSource {
				t.Errorf("Source = %q, want %q (reason %q)", got.Source, tt.wantSource, got.Reason)
			}
			wantHost := tt.wantHost
			if wantHost == "" && tt.wantSource == SourceProfile {
				wantHost = ProfileDockerHost(home, ProfileName)
			}
			if wantHost != "" && got.DockerHost != wantHost {
				t.Errorf("DockerHost = %q, want %q", got.DockerHost, wantHost)
			}
			if got.Started != tt.wantStart {
				t.Errorf("Started = %v, want %v", got.Started, tt.wantStart)
			}
			if tt.wantReason != "" && !strings.Contains(got.Reason, tt.wantReason) {
				t.Errorf("Reason = %q, want it to contain %q", got.Reason, tt.wantReason)
			}
			if tt.wantStart && len(tt.colima.starts) != 1 {
				t.Errorf("start calls = %d, want 1", len(tt.colima.starts))
			}
			if !tt.wantStart && len(tt.colima.starts) != 0 {
				t.Errorf("start calls = %d, want none", len(tt.colima.starts))
			}
		})
	}
}

// A brand new profile is sized; an existing one keeps whatever configuration
// it was given, because resizing a VM someone tuned deliberately is not this
// harness's call to make.
func TestResolveSizesOnlyNewProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     ProfileStatus
		wantCPUs   int
		wantMemory int
	}{
		{
			name:       "absent profile carries the sizing flags",
			status:     ProfileStatus{Name: ProfileName},
			wantCPUs:   ProfileCPUs,
			wantMemory: ProfileMemoryGiB,
		},
		{
			name:   "existing profile starts with its saved configuration",
			status: ProfileStatus{Name: ProfileName, Exists: true, Status: "Stopped"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			colima := &fakeColima{
				status:     tt.status,
				afterStart: ProfileStatus{Name: ProfileName, Exists: true, Running: true},
			}
			if _, err := Resolve(context.Background(), Options{
				Getenv:    env(nil),
				Home:      t.TempDir(),
				Colima:    colima,
				AutoStart: true,
			}); err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if len(colima.starts) != 1 {
				t.Fatalf("start calls = %d, want 1", len(colima.starts))
			}
			if got := colima.starts[0].CPUs; got != tt.wantCPUs {
				t.Errorf("CPUs = %d, want %d", got, tt.wantCPUs)
			}
			if got := colima.starts[0].MemoryGiB; got != tt.wantMemory {
				t.Errorf("MemoryGiB = %d, want %d", got, tt.wantMemory)
			}
		})
	}
}

// The race the start lock exists for: two entrants both see no profile, one
// creates it and leaves it stopped, and the other wakes up holding the lock.
// The waiter must size from what it finds now, not from what it saw before it
// waited, or it hands Colima --cpus/--memory for a VM that already exists and
// silently resizes somebody's machine.
func TestResolveSizesFromThePostLockState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		before     ProfileStatus
		after      ProfileStatus
		wantStarts int
		wantCPUs   int
		wantMemory int
	}{
		{
			name:       "created by another entrant while this one waited",
			before:     ProfileStatus{Name: ProfileName},
			after:      ProfileStatus{Name: ProfileName, Exists: true, Status: "Stopped"},
			wantStarts: 1,
		},
		{
			name:       "still absent after the wait, so this entrant creates it",
			before:     ProfileStatus{Name: ProfileName},
			after:      ProfileStatus{Name: ProfileName},
			wantStarts: 1,
			wantCPUs:   ProfileCPUs,
			wantMemory: ProfileMemoryGiB,
		},
		{
			name:       "started by another entrant while this one waited",
			before:     ProfileStatus{Name: ProfileName},
			after:      ProfileStatus{Name: ProfileName, Exists: true, Running: true},
			wantStarts: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			after := tt.after
			colima := &fakeColima{status: tt.before, later: &after}
			got, err := Resolve(context.Background(), Options{
				Getenv:    env(nil),
				Home:      t.TempDir(),
				Colima:    colima,
				AutoStart: true,
				Out:       io.Discard,
			})
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got.Source != SourceProfile {
				t.Fatalf("Source = %q, want %q", got.Source, SourceProfile)
			}
			if len(colima.starts) != tt.wantStarts {
				t.Fatalf("start calls = %d, want %d", len(colima.starts), tt.wantStarts)
			}
			if tt.wantStarts == 0 {
				return
			}
			if got := colima.starts[0].CPUs; got != tt.wantCPUs {
				t.Errorf("CPUs = %d, want %d (sizing an existing profile silently resizes it)", got, tt.wantCPUs)
			}
			if got := colima.starts[0].MemoryGiB; got != tt.wantMemory {
				t.Errorf("MemoryGiB = %d, want %d", got, tt.wantMemory)
			}
		})
	}
}

// The child process inherits the parent's answer through DOCKER_HOST; it must
// not re-interrogate Colima for a decision already made.
func TestResolveTrustsInheritedProfileSocket(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	socket := ProfileDockerHost(home, ProfileName)
	mustCreate(t, SocketPath(socket))

	colima := &fakeColima{status: ProfileStatus{Name: ProfileName}}
	got, err := Resolve(context.Background(), Options{
		Getenv: env(map[string]string{DockerHostVar: socket}),
		Home:   home,
		Colima: colima,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Source != SourceProfile || got.DockerHost != socket {
		t.Fatalf("Resolve() = %+v, want the inherited profile socket", got)
	}
	if colima.statusCalls != 0 {
		t.Errorf("Colima was consulted %d times, want 0", colima.statusCalls)
	}
}

// An inherited socket that no longer exists means the VM stopped since the
// value was set, so the answer has to be re-derived rather than trusted.
func TestResolveRejectsInheritedSocketThatIsGone(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	colima := &fakeColima{status: ProfileStatus{Name: ProfileName, Exists: true, Running: true}}
	got, err := Resolve(context.Background(), Options{
		Getenv: env(map[string]string{DockerHostVar: ProfileDockerHost(home, ProfileName)}),
		Home:   home,
		Colima: colima,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if colima.statusCalls != 1 {
		t.Errorf("Colima was consulted %d times, want 1", colima.statusCalls)
	}
	if got.Source != SourceProfile {
		t.Errorf("Source = %q, want %q", got.Source, SourceProfile)
	}
}

func TestResolveHonoursCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	colima := &fakeColima{status: ProfileStatus{Name: ProfileName}}
	if _, err := Resolve(ctx, Options{Getenv: env(nil), Home: t.TempDir(), Colima: colima, AutoStart: true}); err == nil {
		t.Fatal("Resolve() with a cancelled context returned no error")
	}
	if len(colima.starts) != 0 {
		t.Errorf("start calls = %d, want 0 on a cancelled context", len(colima.starts))
	}
}

func TestResolutionLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		res      Resolution
		contains []string
	}{
		{
			name:     "dedicated profile names the profile",
			res:      Resolution{DockerHost: "unix:///h/.colima/camp-itest/docker.sock", Profile: ProfileName, Source: SourceProfile},
			contains: []string{ProfileName, "unix:///h/.colima/camp-itest/docker.sock"},
		},
		{
			name:     "a started profile says so",
			res:      Resolution{Profile: ProfileName, Source: SourceProfile, Started: true},
			contains: []string{"started for this run"},
		},
		{
			name:     "fallback shouts",
			res:      Resolution{DockerHost: "unix:///shared.sock", Source: SourceFallback, Reason: "Colima is unavailable"},
			contains: []string{"WARNING", "Colima is unavailable", "collapse"},
		},
		{
			name:     "override names the variable",
			res:      Resolution{DockerHost: "tcp://remote:2375", Source: SourceOverride},
			contains: []string{EnvDockerHost, "tcp://remote:2375"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			line := tt.res.Line()
			for _, want := range tt.contains {
				if !strings.Contains(line, want) {
					t.Errorf("Line() = %q, want it to contain %q", line, want)
				}
			}
		})
	}
}

func TestSocketPathAndSameDockerHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a, b     string
		wantSame bool
		wantPath string
	}{
		{name: "identical unix hosts", a: "unix:///a/docker.sock", b: "unix:///a/docker.sock", wantSame: true, wantPath: "/a/docker.sock"},
		{name: "redundant separators normalize", a: "unix:///a//docker.sock", b: "unix:///a/docker.sock", wantSame: true, wantPath: "/a/docker.sock"},
		{name: "different profiles differ", a: "unix:///a/default/docker.sock", b: "unix:///a/camp-itest/docker.sock", wantPath: "/a/default/docker.sock"},
		{name: "tcp compares literally", a: "tcp://host:2375", b: "tcp://host:2375", wantSame: true},
		{name: "empty is never the same", a: "", b: "unix:///a/docker.sock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sameDockerHost(tt.a, tt.b); got != tt.wantSame {
				t.Errorf("sameDockerHost(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.wantSame)
			}
			if tt.wantPath != "" {
				if got := SocketPath(tt.a); got != tt.wantPath {
					t.Errorf("SocketPath(%q) = %q, want %q", tt.a, got, tt.wantPath)
				}
			}
		})
	}
}

func TestIsColimaSocket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dockerHost string
		want       bool
	}{
		{name: "default profile", dockerHost: "unix:///home/u/.colima/default/docker.sock", want: true},
		{name: "dedicated profile", dockerHost: "unix:///home/u/.colima/camp-itest/docker.sock", want: true},
		{name: "legacy top level socket", dockerHost: "unix:///home/u/.colima/docker.sock", want: true},
		{name: "another user's colima", dockerHost: "unix:///home/other/.colima/default/docker.sock"},
		{name: "native Docker", dockerHost: "unix:///var/run/docker.sock"},
		{name: "Docker Desktop", dockerHost: "unix:///home/u/.docker/run/docker.sock"},
		{name: "remote daemon", dockerHost: "tcp://remote:2375"},
		{name: "not a docker socket", dockerHost: "unix:///home/u/.colima/default/containerd.sock"},
		{name: "unset", dockerHost: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isColimaSocket("/home/u", tt.dockerHost); got != tt.want {
				t.Errorf("isColimaSocket(%q) = %v, want %v", tt.dockerHost, got, tt.want)
			}
		})
	}
}

func TestProfileDockerHost(t *testing.T) {
	t.Parallel()

	got := ProfileDockerHost("/home/u", ProfileName)
	want := "unix://" + filepath.Join("/home/u", ".colima", ProfileName, "docker.sock")
	if got != want {
		t.Fatalf("ProfileDockerHost() = %q, want %q", got, want)
	}
}
