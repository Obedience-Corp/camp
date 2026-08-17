//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"os"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/internal/buildutil/itestenv"
)

// The suite competes for a Docker daemon it does not own, and both halves of
// that were unmanaged until now: which daemon it lands on, and whether another
// run is already using it. A pool sized to the daemon's CPU count is only
// correct if the pool is the only thing on that daemon, which was true when
// the harness was written and stopped being true as residents and concurrent
// agent sessions accumulated on the default profile.
//
// Three steps close that, all before a single container is created:
// resolve a daemon the suite owns, take the lock that keeps a second run off
// it, and refuse to start at all if it is already too slow to finish.

// infraBannerPrefix is the marker the dashboard's tally matches on. Printed to
// stdout rather than stderr on purpose: `go test -json` discards the test
// binary's stderr, and a refusal nobody can see is the failure mode this whole
// pillar exists to end.
const infraBannerPrefix = "INFRASTRUCTURE FAILURE (not a test failure): "

// daemonSession is the run's claim on a Docker daemon: which one, and the
// lock that says this run has it.
type daemonSession struct {
	resolution itestenv.Resolution
	lock       *itestenv.Lock
}

// prepareDaemon resolves the daemon and takes the suite lock for it, before
// any container exists.
//
// The lock is keyed by the daemon rather than by the repository, because the
// daemon is what two runs actually contend for: a camp suite and a fest suite
// pointed at one VM collapse each other exactly as two camp suites would.
func prepareDaemon(ctx context.Context) (*daemonSession, error) {
	resolution, err := itestenv.Resolve(ctx, itestenv.Options{AutoStart: true, Out: os.Stdout})
	if err != nil {
		return nil, camperrors.Wrap(err, "resolve the integration Docker daemon")
	}
	fmt.Fprintln(os.Stderr, resolution.Line())
	if resolution.Source == itestenv.SourceFallback {
		// Also on stdout: in the dashboard lane stderr is discarded, and the
		// one fact that explains a later collapse is that the run shared a
		// daemon with everything else on the machine.
		fmt.Println(resolution.Line())
	}
	if resolution.DockerHost != "" {
		if err := os.Setenv(itestenv.DockerHostVar, resolution.DockerHost); err != nil {
			return nil, camperrors.Wrapf(err, "publish %s", itestenv.DockerHostVar)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, camperrors.Wrap(err, "resolve the home directory for the suite lock")
	}
	lock, err := itestenv.Acquire(ctx, itestenv.SuiteLockPath(home, resolution.DockerHost),
		itestenv.LockOptions{
			Wait:  itestenv.LockWait(os.Getenv),
			Out:   os.Stdout,
			Label: "camp integration suite on " + daemonLabel(resolution),
		})
	if err != nil {
		return nil, err
	}
	return &daemonSession{resolution: resolution, lock: lock}, nil
}

// release drops the suite lock. Failing to release is reported and not fatal:
// the kernel releases the flock when this process exits either way.
func (d *daemonSession) release() {
	if d == nil {
		return
	}
	if err := d.lock.Release(); err != nil {
		fmt.Fprintln(os.Stderr, "could not release the suite lock:", err)
	}
}

func daemonLabel(resolution itestenv.Resolution) string {
	if resolution.Profile != "" {
		return resolution.Profile
	}
	if resolution.DockerHost != "" {
		return resolution.DockerHost
	}
	return "the default Docker daemon"
}

// probeDaemon refuses a run against a daemon that is already degraded.
//
// Refusing costs seconds. Not refusing costs the run: on 2026-08-05 the suite
// dispatched into a daemon that was already wedged and discovered it one
// parked exec at a time, ending in a timeout panic with no failing assertion
// anywhere in the output.
func probeDaemon(ctx context.Context, dockerHost string) error {
	result := itestenv.Probe(ctx, dockerHost)
	fmt.Fprintln(os.Stderr, result.Line())
	if degraded, reason := result.Degraded(); degraded {
		return camperrors.New(reason)
	}
	return nil
}

// reportTransportFallback explains that the run is using Colima's own Docker
// socket rather than a tunnel of its own.
//
// The tunnel is not always available: a Lima guest image whose sshd will not
// forward unix sockets (observed on Ubuntu 24.04.4 with OpenSSH
// 9.6p1-3ubuntu13.16, where TCP forwarding works and stream-local forwarding
// opens a channel the server never confirms) leaves the suite with nothing to
// tunnel through. Before this the run died there, which meant a freshly
// created profile could not run the suite at all.
func reportTransportFallback(cause error) {
	notice := "dedicated Docker transport unavailable (" + cause.Error() + "); " +
		"continuing on Colima's own socket for this run"
	fmt.Fprintln(os.Stderr, notice)
	fmt.Println(notice)
}

// reportInfrastructureRefusal states, once, that the run did not happen, and
// what to do about it. The dashboard renders this as the non-run verdict; a
// human reading the raw output gets the same sentence.
func reportInfrastructureRefusal(reason string) {
	fmt.Println(infraBannerPrefix + reason)
	fmt.Printf("The integration run did not happen: no test executed. "+
		"Check the daemon with 'just test integration-doctor', or set %s to "+
		"point the suite somewhere else.\n", itestenv.EnvDockerHost)
}
