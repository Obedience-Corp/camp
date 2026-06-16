package git

import (
	"bytes"
	"context"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// StatusEntry is one entry from git status porcelain -z output.
type StatusEntry struct {
	Code string
	Path string
}

// StatusPorcelain runs git status --porcelain=v1 -z and returns raw output.
// Unlike Output, it preserves leading whitespace in XY status codes.
func StatusPorcelain(ctx context.Context, repoPath string, extraArgs ...string) ([]byte, error) {
	args := []string{"status", "--porcelain=v1", "-z"}
	args = append(args, extraArgs...)
	cmd := gitCmd(ctx, repoPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrapf(err, "git status --porcelain=v1 -z: %s", strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

// ParseStatusPorcelainZ parses NUL-delimited porcelain v1 status output.
func ParseStatusPorcelainZ(out []byte) []StatusEntry {
	fields := splitNULFields(out)
	entries := make([]StatusEntry, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if len(field) == 0 {
			continue
		}
		if len(field) < 4 {
			continue
		}
		code := string(field[:2])
		path := string(field[3:])
		entries = append(entries, StatusEntry{Code: code, Path: path})
		if code[0] == 'R' || code[0] == 'C' {
			i++ // -z emits the old path as a second NUL-delimited field.
		}
	}
	return entries
}

func splitNULFields(out []byte) [][]byte {
	return bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
}
