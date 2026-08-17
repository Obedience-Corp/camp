// internal/buildutil/tasks/integration_doctor.go
package tasks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/internal/buildutil/itestenv"
	"github.com/Obedience-Corp/camp/internal/buildutil/ui"
)

const (
	bytesPerGiB     = 1 << 30
	dockerPSTimeout = 15 * time.Second
	// namesShown keeps the container row inside the summary card; the count is
	// the part that matters, the names are a hint about who to go and ask.
	namesShown = 2
)

// IntegrationDoctor reports whether this machine can run the integration suite
// without collapsing: which daemon the suite would use, whether that daemon is
// answering, who else is on it, and whether another run already holds the
// suite lock.
//
// It is read-only unless start is set. Inspecting an environment must not
// change it, and a report that silently boots a VM cannot be used to answer
// "what state is this machine in".
func IntegrationDoctor(ctx context.Context, start bool) error {
	began := time.Now()
	ui.Section("Integration Daemon Doctor")

	resolution, err := itestenv.Resolve(ctx, itestenv.Options{AutoStart: start, Out: os.Stdout})
	if err != nil {
		return camperrors.Wrap(err, "resolve the integration Docker daemon")
	}

	if resolution.Source == itestenv.SourceFallback {
		ui.Warning(resolution.Line())
	} else {
		fmt.Printf("  %s\n", resolution.Line())
	}

	probe := itestenv.Probe(ctx, resolution.DockerHost)
	degraded, reason := probe.Degraded()

	rows := [][]string{
		{"Check", "Result"},
		{"Profile", profileRow(ctx)},
		{"Docker host", hostRow(resolution)},
		{"Daemon probe", probeRow(probe)},
		{"Containers", containersRow(ctx, resolution)},
		{"Suite lock", lockRow(resolution)},
	}
	ui.SummaryCardWithStatus("Integration Daemon", rows,
		fmt.Sprintf("%.2fs", time.Since(began).Seconds()), !degraded,
		"✓ DAEMON READY", "✗ DAEMON NOT USABLE")

	if degraded {
		return camperrors.Newf("the integration daemon is not usable: %s", reason)
	}
	if resolution.Source == itestenv.SourceFallback {
		ui.Warning("no dedicated daemon: run 'just test daemon-start' to create or boot the " +
			itestenv.ProfileName + " profile")
	}
	return nil
}

func hostRow(resolution itestenv.Resolution) string {
	host := shortHome(resolution.DockerHost)
	if host == "" {
		host = "Docker's default socket"
	}
	if resolution.Source == itestenv.SourceFallback {
		return "shared: " + host
	}
	return host
}

// shortHome keeps a summary row inside the card by writing paths the way a
// shell prompt would.
func shortHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	return strings.Replace(path, home, "~", 1)
}

func probeRow(probe itestenv.ProbeResult) string {
	row := strings.TrimPrefix(probe.Line(), "daemon probe: ")
	if host := probe.DockerHost; host != "" {
		row = strings.TrimPrefix(row, host+" ")
		row = strings.TrimPrefix(row, "Docker's default socket ")
	}
	if degraded, reason := probe.Degraded(); degraded {
		return row + " - " + reason
	}
	return row
}

func profileRow(ctx context.Context) string {
	profile := itestenv.ConfiguredProfile(nil)
	if profile == itestenv.ProfileDisabled {
		return "isolation disabled by " + itestenv.EnvProfile
	}
	status, err := itestenv.NewColima().Status(ctx, profile)
	if err != nil {
		return profile + ": Colima unavailable (" + err.Error() + ")"
	}
	row := profile + ": " + status.State()
	if status.Exists {
		row += ", " + strconv.Itoa(status.CPUs) + " CPUs, " +
			strconv.FormatFloat(float64(status.MemoryBytes)/bytesPerGiB, 'f', 1, 64) + " GiB"
	}
	if !status.Running {
		row += " (fix: just test daemon-start)"
	}
	return row
}

// containersRow names what is running on the suite's daemon. On the dedicated
// profile the expected answer is nothing: anything else there is a co-tenant
// competing for the CPUs the pool sized itself against.
func containersRow(ctx context.Context, resolution itestenv.Resolution) string {
	ctx, cancel := context.WithTimeout(ctx, dockerPSTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "ps", "--format", "{{.Names}}")
	cmd.Env = os.Environ()
	if resolution.DockerHost != "" {
		cmd.Env = append(cmd.Env, dockerHostEnv+"="+resolution.DockerHost)
	}
	out, err := cmd.Output()
	if err != nil {
		return "could not list containers: " + err.Error()
	}
	names := strings.Fields(strings.TrimSpace(string(out)))
	if len(names) == 0 {
		return "none running"
	}
	// On the dedicated profile anything here is a co-tenant competing for the
	// CPUs the pool sized itself against, which is the condition this whole
	// check exists to make visible.
	row := strconv.Itoa(len(names)) + " running: " + strings.Join(names[:min(len(names), namesShown)], ", ")
	if len(names) > namesShown {
		row += ", +" + strconv.Itoa(len(names)-namesShown) + " more"
	}
	return row
}

func lockRow(resolution itestenv.Resolution) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "unknown: " + err.Error()
	}
	path := itestenv.SuiteLockPath(home, resolution.DockerHost)
	_, description := itestenv.LockStatus(path)
	return description + " (" + shortHome(path) + ")"
}
