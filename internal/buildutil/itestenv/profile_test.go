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
}

func (f *fakeColima) Status(_ context.Context, profile string) (ProfileStatus, error) {
	f.statusCalls++
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
			vars:       map[string]string{EnvProfile: ProfileDisabled, dockerHostEnv: "unix:///shared/docker.sock"},
			colima:     &fakeColima{status: running},
			wantSource: SourceFallback,
			wantHost:   "unix:///shared/docker.sock",
			wantReason: EnvProfile + "=" + ProfileDisabled,
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
			name:       "no colima falls back loudly",
			colima:     &fakeColima{statusErr: io.ErrUnexpectedEOF},
			autoStart:  true,
			wantSource: SourceFallback,
			wantReason: "Colima is unavailable",
		},
		{
			name:       "failed start falls back rather than failing the run",
			colima:     &fakeColima{status: absent, startErr: io.ErrClosedPipe},
			autoStart:  true,
			wantSource: SourceFallback,
			wantReason: "could not start profile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			got, err := Resolve(context.Background(), Options{
				Getenv:    env(tt.vars),
				Home:      home,
				Colima:    tt.colima,
				AutoStart: tt.autoStart,
				Out:       io.Discard,
			})
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

// The child process inherits the parent's answer through DOCKER_HOST; it must
// not re-interrogate Colima for a decision already made.
func TestResolveTrustsInheritedProfileSocket(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	socket := ProfileDockerHost(home, ProfileName)
	mustCreate(t, SocketPath(socket))

	colima := &fakeColima{status: ProfileStatus{Name: ProfileName}}
	got, err := Resolve(context.Background(), Options{
		Getenv: env(map[string]string{dockerHostEnv: socket}),
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
		Getenv: env(map[string]string{dockerHostEnv: ProfileDockerHost(home, ProfileName)}),
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

func TestProfileDockerHost(t *testing.T) {
	t.Parallel()

	got := ProfileDockerHost("/home/u", ProfileName)
	want := "unix://" + filepath.Join("/home/u", ".colima", ProfileName, "docker.sock")
	if got != want {
		t.Fatalf("ProfileDockerHost() = %q, want %q", got, want)
	}
}
