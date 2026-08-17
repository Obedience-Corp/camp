//go:build integration && (darwin || linux)

package integration

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Obedience-Corp/camp/internal/buildutil/itestenv"
)

const (
	colimaTransportStartupTimeout = 10 * time.Second
	tunnelPingTimeout             = 500 * time.Millisecond
)

// startDedicatedColimaDockerTransport isolates the integration suite's Docker
// traffic from Colima's lifecycle ControlMaster. Lima normally forwards
// Colima's Docker socket over the same long-lived SSH process that owns VM port
// forwarding. Hundreds of concurrent Docker exec streams can kill that SSH
// process and leave Colima running with an unusable host socket. The suite uses
// its own SSH process instead, so its load and lifecycle cannot take down the
// user's global Docker transport.
func startDedicatedColimaDockerTransport() (func(), error) {
	sshConfig, sshHost, ok := colimaSSHConfig(os.Getenv("DOCKER_HOST"))
	if !ok {
		return func() {}, nil
	}
	if _, err := os.Stat(sshConfig); err != nil {
		return nil, fmt.Errorf("Colima SSH config %s is unavailable: %w", sshConfig, err)
	}

	dir, err := os.MkdirTemp("", "camp-colima-docker-*")
	if err != nil {
		return nil, fmt.Errorf("create transport directory: %w", err)
	}
	socket := filepath.Join(dir, "docker.sock")

	cmd := exec.Command("ssh",
		"-F", sshConfig,
		"-o", "ControlMaster=no",
		"-o", "ControlPath=none",
		"-o", "ExitOnForwardFailure=yes",
		"-N",
		"-L", socket+":/var/run/docker.sock",
		sshHost,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	restoreLimit := raiseOpenFileLimitForChild()
	if err := cmd.Start(); err != nil {
		restoreLimit()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("start Colima SSH transport: %w", err)
	}
	restoreLimit()

	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
	}()

	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(os.Interrupt)
			}
			select {
			case <-exited:
			case <-time.After(2 * time.Second):
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				<-exited
			}
			_ = os.RemoveAll(dir)
		})
	}

	deadline := time.Now().Add(colimaTransportStartupTimeout)
	for {
		if err := pingTunnel(socket); err == nil {
			break
		}
		select {
		case err := <-exited:
			_ = os.RemoveAll(dir)
			return nil, fmt.Errorf("Colima SSH transport exited during startup: %v: %s", err, strings.TrimSpace(stderr.String()))
		default:
		}
		if time.Now().After(deadline) {
			stop()
			return nil, fmt.Errorf("Colima SSH transport was not ready after %s: %s", colimaTransportStartupTimeout, strings.TrimSpace(stderr.String()))
		}
		time.Sleep(50 * time.Millisecond)
	}

	previousDockerHost := os.Getenv("DOCKER_HOST")
	if err := os.Setenv("DOCKER_HOST", "unix://"+socket); err != nil {
		stop()
		return nil, fmt.Errorf("set isolated DOCKER_HOST: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Integration Docker transport: isolated Colima tunnel at %s\n", socket)
	return func() {
		_ = os.Setenv("DOCKER_HOST", previousDockerHost)
		stop()
	}, nil
}

func colimaSSHConfig(dockerHost string) (config, host string, ok bool) {
	u, err := url.Parse(dockerHost)
	if err != nil || u.Scheme != "unix" {
		return "", "", false
	}
	clean := filepath.Clean(u.Path)
	marker := string(filepath.Separator) + ".colima" + string(filepath.Separator)
	markerIndex := strings.Index(clean, marker)
	if markerIndex < 0 || filepath.Base(clean) != "docker.sock" {
		return "", "", false
	}

	colimaHome := filepath.Join(clean[:markerIndex], ".colima")
	relative, err := filepath.Rel(colimaHome, clean)
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(relative, string(filepath.Separator))
	profile := "default"
	if len(parts) == 2 {
		profile = parts[0]
	} else if len(parts) != 1 {
		return "", "", false
	}

	instance := "colima"
	if profile != "default" {
		instance += "-" + profile
	}
	return filepath.Join(colimaHome, "_lima", instance, "ssh.config"), "lima-" + instance, true
}

// pingTunnel checks whether the freshly started tunnel is carrying Docker API
// traffic yet. The budget is short because this runs in a startup poll loop:
// a socket that is not listening yet must be retried, not waited on.
func pingTunnel(socket string) error {
	ctx, cancel := context.WithTimeout(context.Background(), tunnelPingTimeout)
	defer cancel()
	return itestenv.Ping(ctx, "unix://"+socket)
}

// raiseOpenFileLimitForChild temporarily raises this process's soft descriptor
// limit so the SSH child inherits enough capacity for parallel Docker streams.
// The parent limit is restored immediately after the child starts.
func raiseOpenFileLimitForChild() func() {
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &original); err != nil {
		return func() {}
	}
	raised := original
	if raised.Cur < 4096 {
		raised.Cur = 4096
		if raised.Cur > raised.Max {
			raised.Cur = raised.Max
		}
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &raised); err != nil {
			return func() {}
		}
	}
	return func() {
		_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &original)
	}
}
