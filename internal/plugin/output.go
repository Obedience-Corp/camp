package plugin

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ErrOutputTooLarge reports a plugin that printed more than the caller allows.
var ErrOutputTooLarge = camperrors.New("plugin output exceeds the limit")

// Output runs a plugin for its answer: no terminal input, stderr discarded,
// run from the temporary directory with CAMP_ROOT and PWD removed from its
// environment, so the answer cannot depend on the camp. It returns at most
// limit bytes of stdout. Stdout is returned even when the plugin exits
// non-zero, with a CommandError. Past the limit the plugin is stopped and
// ErrOutputTooLarge is returned. Context cancellation uses the same interrupt
// and grace period as Execute, and the grace period also bounds a child that
// keeps the plugin's stdout open after the plugin exits.
func Output(ctx context.Context, p Plugin, args []string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()

	cmd := exec.CommandContext(runCtx, p.Path, args...)
	cmd.Dir = os.TempDir()
	cmd.Env = withoutEnv(withoutEnv(os.Environ(), EnvCampRoot), "PWD")
	stdout := &cappedBuffer{limit: limit, stop: stop}
	cmd.Stdout = stdout
	configurePluginCancel(cmd)
	err := cmd.Run()

	switch {
	case ctx.Err() != nil:
		return stdout.buf.Bytes(), ctx.Err()
	case stdout.over:
		return stdout.buf.Bytes(), ErrOutputTooLarge
	case errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success():
		// The plugin answered and exited 0; a child it left behind held stdout
		// open until the grace period closed it. The answer is complete.
		return stdout.buf.Bytes(), nil
	case err != nil:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.buf.Bytes(), camperrors.NewCommand(Prefix+p.Name, exitErr.ExitCode(), "", exitErr)
		}
		return stdout.buf.Bytes(), camperrors.Wrap(err, "run plugin "+p.Name)
	}
	return stdout.buf.Bytes(), nil
}

// cappedBuffer keeps at most limit bytes. Past the limit it stops the plugin
// and refuses further output, so a flooding plugin cannot hold camp.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int64
	over  bool
	stop  context.CancelFunc
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	room := c.limit - int64(c.buf.Len())
	if int64(len(p)) <= room {
		return c.buf.Write(p)
	}
	c.buf.Write(p[:max(room, 0)])
	c.over = true
	c.stop()
	return 0, ErrOutputTooLarge
}

func withoutEnv(env []string, key string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, key) {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}
