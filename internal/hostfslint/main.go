package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

var errNewViolators = camperrors.New("lint-no-host-fs-tests: new violators")

func main() {
	ctx := context.Background()
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	if err := run(ctx, root, defaultAllowlist, os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, errNewViolators) {
			fmt.Fprintln(os.Stderr, err.Error())
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, root string, allowlist []string, stdout, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	report, err := Scan(ctx, root, allowlist)
	if err != nil {
		return camperrors.Wrap(err, "lint-no-host-fs-tests")
	}
	if len(report.New) > 0 {
		if err := writeFail(stderr, report.New); err != nil {
			return err
		}
		return errNewViolators
	}
	_, err = fmt.Fprintf(stdout, "lint-no-host-fs-tests: clean (no NEW violators; %d legacy files on allowlist)\n", len(report.Hits))
	return err
}

func writeFail(stderr io.Writer, violators []string) error {
	if _, err := fmt.Fprintln(stderr, "FAIL: NEW host-side git exec.Command in _test.go outside tests/integration/:"); err != nil {
		return camperrors.Wrap(err, "hostfslint: write")
	}
	for _, v := range violators {
		if _, err := fmt.Fprintf(stderr, "  %s\n", v); err != nil {
			return camperrors.Wrap(err, "hostfslint: write")
		}
	}
	_, err := fmt.Fprint(stderr, `
Fix by either:
  - moving it to tests/integration/ (GetSharedContainer + RunCampInDir), or
  - tagging the file //go:build container_fs and registering its package
    in containerFSPackages (tests/integration/containerfs_test.go), or
  - (if it is genuinely pure-logic) adding it to defaultAllowlist in
    internal/hostfslint/allowlist.go.
`)
	if err != nil {
		return camperrors.Wrap(err, "hostfslint: write")
	}
	return nil
}
