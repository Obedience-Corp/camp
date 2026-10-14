//go:build integration && windows

package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// windowsStarterContainerImage is process-isolated on windows-2025 hosts per
// Microsoft's Server 2025 / LTSC 2025 container compatibility matrix.
const windowsStarterContainerImage = "mcr.microsoft.com/windows/servercore:ltsc2025"

var windowsStarterRunLine = regexp.MustCompile(`(?m)^=== RUN\s+(Test[A-Za-z0-9_]+)$`)

// TestWindowsStarterLockInContainer compiles the starter container_fs suite for
// Windows and executes it inside a Windows Server Core container. Fixture
// directories come from the test binary's t.TempDir calls, which land on the
// container filesystem rather than the hosted runner workspace.
func TestWindowsStarterLockInContainer(t *testing.T) {
	root := repoRootForWindowsStarter()
	workHost, err := os.MkdirTemp("", "camp-windows-starter-proof-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(workHost) })

	binPath := filepath.Join(workHost, "starter.test.exe")
	compile := exec.Command("go", "test", "-c",
		"-tags", containerFSTag,
		"-o", binPath,
		"./internal/starter",
	)
	compile.Dir = root
	compile.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := compile.CombinedOutput()
	require.NoError(t, err, "compile starter container_fs tests:\n%s", out)

	containerWork := `C:\camp-starter-proof`
	containerBin := containerWork + `\starter.test.exe`

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	create := exec.CommandContext(ctx, "docker", "create",
		windowsStarterContainerImage,
		"cmd", "/c", "ping", "-n", "3600", "127.0.0.1", ">", "nul",
	)
	var createOut bytes.Buffer
	create.Stdout = &createOut
	create.Stderr = &createOut
	require.NoError(t, create.Run(), "docker create:\n%s", createOut.String())
	containerID := strings.TrimSpace(createOut.String())
	require.NotEmpty(t, containerID)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", containerID).Run()
	})

	require.NoError(t, exec.CommandContext(ctx, "docker", "start", containerID).Run())
	require.NoError(t, dockerMkdir(ctx, containerID, containerWork))

	require.NoError(t, exec.CommandContext(ctx, "docker", "cp", binPath, containerID+":"+containerBin).Run())

	testCmd := strings.Join([]string{
		"set HOME=" + containerWork,
		"set USERPROFILE=" + containerWork,
		"set TEMP=" + containerWork + `\tmp`,
		"set TMP=" + containerWork + `\tmp`,
		"mkdir " + containerWork + `\tmp`,
		"cd /d " + containerWork,
		containerBin + " -test.v -test.count=1",
	}, " && ")

	execOut, err := dockerExec(ctx, containerID, testCmd)
	require.NoError(t, err, "starter suite in Windows container:\n%s", execOut)

	ran := windowsStarterRunLine.FindAllStringSubmatch(execOut, -1)
	require.GreaterOrEqualf(t, len(ran), 7,
		"expected at least 7 starter tests on Windows LockFileEx path; output:\n%s", execOut)
	require.Contains(t, execOut, "PASS")
	t.Logf("Windows container starter proof: %d tests executed in %s", len(ran), windowsStarterContainerImage)
}

func repoRootForWindowsStarter() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return filepath.Join(wd, "..", "..")
}

func dockerMkdir(ctx context.Context, containerID, dir string) error {
	_, err := dockerExec(ctx, containerID, "mkdir "+dir)
	return err
}

func dockerExec(ctx context.Context, containerID, cmdLine string) (string, error) {
	execCmd := exec.CommandContext(ctx, "docker", "exec", containerID, "cmd", "/c", cmdLine)
	out, err := execCmd.CombinedOutput()
	return string(out), err
}
