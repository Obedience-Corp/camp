package skills

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/git"
)

const backupStampLayout = "20060102T150405Z"

func secureUndo(ctx context.Context, root, skillsDir string, stamp time.Time, files []FileOutcome) ([]Undo, error) {
	var order []string
	replaced := make(map[string][]FileOutcome)
	for _, out := range files {
		if out.Action != ActionReplace {
			continue
		}
		if _, seen := replaced[out.Bundle]; !seen {
			order = append(order, out.Bundle)
		}
		replaced[out.Bundle] = append(replaced[out.Bundle], out)
	}

	undos := make([]Undo, 0, len(order))
	for _, bundle := range order {
		if err := ctx.Err(); err != nil {
			return undos, err
		}
		bundleRel := shippedSkillsPrefix + bundle
		if gitRestores(ctx, root, bundleRel, replaced[bundle]) {
			undos = append(undos, Undo{Bundle: bundle, Command: "git checkout HEAD -- " + bundleRel})
			continue
		}
		dir, err := backupReplaced(skillsDir, bundle+"-"+stamp.Format(backupStampLayout), replaced[bundle])
		if err != nil {
			return undos, err
		}
		backupRel := shippedSkillsPrefix + BackupDirName + "/" + filepath.Base(dir)
		undos = append(undos, Undo{
			Bundle:    bundle,
			Command:   "cp -R " + backupRel + "/. " + bundleRel + "/",
			BackupDir: backupRel,
		})
	}
	return undos, nil
}

func gitRestores(ctx context.Context, root, bundleRel string, files []FileOutcome) bool {
	if _, err := git.Output(ctx, root, "diff", "--quiet", "HEAD", "--", bundleRel); err != nil {
		return false
	}
	for _, out := range files {
		if _, err := git.Output(ctx, root, "cat-file", "-e", "HEAD:./"+out.RelPath()); err != nil {
			return false
		}
	}
	return true
}

func backupReplaced(skillsDir, name string, files []FileOutcome) (string, error) {
	base := filepath.Join(skillsDir, BackupDirName)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", camperrors.Wrap(err, "create skills backup directory")
	}
	dir, err := reserveDir(base, name)
	if err != nil {
		return "", err
	}
	for _, out := range files {
		rel := filepath.FromSlash(out.Path)
		data, err := os.ReadFile(filepath.Join(skillsDir, out.Bundle, rel))
		if err != nil {
			return "", camperrors.Wrapf(err, "read %s for backup", out.RelPath())
		}
		dest := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", camperrors.Wrapf(err, "create backup directory for %s", out.RelPath())
		}
		if err := fsutil.WriteFileAtomically(dest, data, 0o644); err != nil {
			return "", camperrors.Wrapf(err, "back up %s", out.RelPath())
		}
	}
	return dir, nil
}

func reserveDir(base, name string) (string, error) {
	for i := 0; ; i++ {
		candidate := name
		if i > 0 {
			candidate = name + "-" + strconv.Itoa(i)
		}
		dir := filepath.Join(base, candidate)
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", camperrors.Wrapf(err, "create backup %s", dir)
		}
	}
}
