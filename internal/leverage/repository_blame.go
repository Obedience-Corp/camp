package leverage

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

type manifestOwnership struct {
	name  string
	lines int
}
type manifestBlame struct {
	path    string
	owners  map[string]manifestOwnership
	buckets map[string]int
	err     error
}

func collectManifestBlame(ctx context.Context, dir, root string, files []ScannedFile, headFiles map[string]bool) ([]manifestBlame, error) {
	results := make([]manifestBlame, len(files))
	jobs := make(chan int)
	workers := min(8, runtime.NumCPU(), len(files))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = blameManifestFile(ctx, dir, root, files[i], headFiles)
			}
		}()
	}
	for i := range files {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func blameManifestFile(ctx context.Context, dir, root string, file ScannedFile, headFiles map[string]bool) manifestBlame {
	r := manifestBlame{path: file.Location, owners: map[string]manifestOwnership{}, buckets: map[string]int{}}
	full := filepath.Join(dir, filepath.FromSlash(file.Location))
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return unavailableBlame(r, file.Lines, err)
	}
	for d := filepath.Dir(full); d != root; d = filepath.Dir(d) {
		if d == filepath.Dir(d) {
			return unavailableBlame(r, file.Lines, camperrors.New("file is outside repository"))
		}
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return unavailableBlame(r, file.Lines, camperrors.New("nested repository attribution is unavailable"))
		}
	}
	if !headFiles[filepath.ToSlash(rel)] {
		r.buckets["uncommitted"] = file.Lines
		return r
	}
	if file.Lines == 0 {
		return r
	}
	out, err := reportGit(ctx, root, "blame", "--line-porcelain", "--", filepath.ToSlash(rel))
	if err != nil {
		return unavailableBlame(r, file.Lines, err)
	}
	parsed, err := parseManifestBlame(out, file.Lines)
	if err != nil {
		return unavailableBlame(r, file.Lines, err)
	}
	parsed.path = file.Location
	return parsed
}

func unavailableBlame(r manifestBlame, lines int, err error) manifestBlame {
	r.err = err
	r.owners = map[string]manifestOwnership{}
	r.buckets = map[string]int{"unavailable": lines}
	return r
}

func parseManifestBlame(out string, want int) (manifestBlame, error) {
	r := manifestBlame{owners: map[string]manifestOwnership{}, buckets: map[string]int{}}
	email, name, hash := "", "", ""
	count := 0
	expectHeader := true
	for _, line := range strings.Split(out, "\n") {
		if expectHeader {
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 3 || (len(fields[0]) != 40 && len(fields[0]) != 64) {
				return r, camperrors.New("invalid blame header")
			}
			if _, err := strconv.Atoi(fields[1]); err != nil {
				return r, camperrors.New("invalid blame line number")
			}
			hash = fields[0]
			email = ""
			name = ""
			expectHeader = false
			continue
		}
		switch {
		case strings.HasPrefix(line, "author "):
			name = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-mail "):
			email = strings.Trim(strings.TrimPrefix(line, "author-mail "), "<>")
		case strings.HasPrefix(line, "\t"):
			count++
			if strings.Trim(hash, "0") == "" {
				r.buckets["uncommitted"]++
			} else {
				if email == "" {
					return r, camperrors.New("blame line has no author")
				}
				a := r.owners[email]
				a.name = name
				a.lines++
				r.owners[email] = a
			}
			expectHeader = true
		}
	}
	if !expectHeader || count != want {
		return r, camperrors.Newf("blame line count %d does not match scan count %d", count, want)
	}
	return r, nil
}
