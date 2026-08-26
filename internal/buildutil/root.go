package main

import (
	"bytes"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const campModulePath = "module github.com/Obedience-Corp/camp"

func findCampRepoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", camperrors.Wrap(err, "resolve camp repo root")
	}
	dir = filepath.Clean(dir)
	for {
		goModPath := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(goModPath)
		if err == nil {
			if bytes.Contains(data, []byte(campModulePath)) {
				return dir, nil
			}
		} else if !os.IsNotExist(err) {
			return "", camperrors.Wrapf(err, "read %s", goModPath)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", camperrors.Wrapf(camperrors.ErrNotFound, "cannot find camp repo root from cwd %s", start)
}
