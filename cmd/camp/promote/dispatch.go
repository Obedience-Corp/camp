package promote

import (
	"context"
	"os"
	"os/exec"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fest"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

type commandRunner interface {
	run(ctx context.Context, dir, bin string, args []string) error
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, dir, bin string, args []string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return camperrors.Wrapf(err, "dispatching %s %v", bin, args)
	}
	return nil
}

var runner commandRunner = execRunner{}

func campBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", camperrors.Wrap(err, "locating camp binary")
	}
	return exe, nil
}

var festLookup = fest.FindFestCLI

func festBinary() (string, error) {
	p, err := festLookup()
	if err != nil {
		return "", camperrors.Wrap(err, "locating fest binary")
	}
	return p, nil
}

type promoteKind int

const (
	kindIntent promoteKind = iota
	kindWorkitem
	kindFestival
)

func kindForType(wt workitem.WorkflowType) promoteKind {
	switch wt {
	case workitem.WorkflowTypeIntent:
		return kindIntent
	case workitem.WorkflowTypeFestival:
		return kindFestival
	default:
		return kindWorkitem
	}
}

func dispatchIntent(ctx context.Context, id, target string) error {
	bin, err := campBinary()
	if err != nil {
		return err
	}
	args := []string{"intent", "promote", id, "--target", target}
	return runner.run(ctx, "", bin, args)
}

func dispatchWorkitem(ctx context.Context, dir, target string, pass []string) error {
	bin, err := campBinary()
	if err != nil {
		return err
	}
	args := []string{"workitem", "promote", "--target", target}
	args = append(args, pass...)
	return runner.run(ctx, dir, bin, args)
}

func dispatchFestival(ctx context.Context, dir string, festArgs []string) error {
	bin, err := festBinary()
	if err != nil {
		return err
	}
	args := []string{"promote"}
	args = append(args, festArgs...)
	return runner.run(ctx, dir, bin, args)
}

func festPassthrough(target string, pass []string) []string {
	var out []string
	if target != "" {
		out = append(out, "--dungeon", target)
	}
	for _, f := range pass {
		switch f {
		case "--force", "--json", "--no-commit":
			out = append(out, f)
		}
	}
	return out
}
