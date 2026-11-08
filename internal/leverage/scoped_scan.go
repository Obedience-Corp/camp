package leverage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ScannedFile describes exactly one file included in the estimate.
type ScannedFile struct {
	Location string
	Lines    int
	Code     int
	Comment  int
	Blank    int
}

type ConfigSource struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// ScopedScan keeps the manifest and estimate from the same scc invocation.
type ScopedScan struct {
	Result         *SCCResult
	Files          []ScannedFile
	Arguments      []string
	Version        string
	ManifestDigest string
}

type ScopedRunner interface {
	Scan(context.Context, string, []string) (*ScopedScan, error)
}

func (r *SCCRunner) Scan(ctx context.Context, dir string, excludes []string) (*ScopedScan, error) {
	args := []string{"--by-file", "--format", FormatJSON2, "--output", "", "--format-multi", "", "--cocomo-project-type", r.cocomoType}
	for _, d := range mergeExcludeDirs(DefaultExcludeDirs, excludes) {
		args = append(args, "--exclude-dir", d)
	}
	args = append(args, dir)
	cmd := exec.CommandContext(ctx, r.binaryPath, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrap(err, "scanning repository with scc")
	}
	scan, err := decodeScopedScan(dir, out)
	if err != nil {
		return nil, err
	}
	scan.Arguments = args
	version, err := exec.CommandContext(ctx, r.binaryPath, "--version").Output()
	if err != nil {
		return nil, camperrors.Wrap(err, "reading scc version")
	}
	scan.Version = strings.TrimSpace(string(version))
	return scan, nil
}

func decodeScopedScan(dir string, data []byte) (*ScopedScan, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, camperrors.Wrap(err, "reading scc JSON")
	}
	for _, key := range []string{"languageSummary", "estimatedPeople", "estimatedScheduleMonths", "estimatedCost"} {
		if _, ok := envelope[key]; !ok {
			return nil, camperrors.Newf("scc output is missing %s; upgrade scc", key)
		}
	}
	var result SCCResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, camperrors.Wrap(err, "reading scc per-file JSON (upgrade scc if unsupported)")
	}
	for _, n := range []float64{result.EstimatedPeople, result.EstimatedScheduleMonths, result.EstimatedCost} {
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return nil, camperrors.New("invalid scc estimate")
		}
	}
	scan := &ScopedScan{Result: &result, Files: []ScannedFile{}}
	seen := map[string]bool{}
	for _, lang := range result.LanguageSummary {
		var lines, code, comments, blanks int
		if len(lang.Files) != lang.Count {
			return nil, camperrors.New("scc per-file manifest is incomplete; install an scc version supporting --by-file --format json2")
		}
		for _, file := range lang.Files {
			rel, err := scanRelativePath(dir, file.Location)
			if err != nil {
				return nil, err
			}
			if seen[rel] {
				return nil, camperrors.Newf("duplicate scc manifest path: %s", rel)
			}
			seen[rel] = true
			if file.Lines < 0 || file.Code < 0 || file.Comment < 0 || file.Blank < 0 {
				return nil, camperrors.New("negative scc file count")
			}
			file.Location = rel
			scan.Files = append(scan.Files, file)
			lines += file.Lines
			code += file.Code
			comments += file.Comment
			blanks += file.Blank
		}
		if lines != lang.Lines || code != lang.Code || comments != lang.Comment || blanks != lang.Blank {
			return nil, camperrors.New("scc file counts do not match language totals")
		}
	}
	sort.Slice(scan.Files, func(i, j int) bool { return scan.Files[i].Location < scan.Files[j].Location })
	scan.ManifestDigest = digestJSON(scan.Files)
	return scan, nil
}

func scanRelativePath(dir, path string) (string, error) {
	if path == "" {
		return "", camperrors.New("scc returned an empty file path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return "", camperrors.Wrap(err, "resolving scan path")
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", camperrors.Newf("scc path escapes target: %s", path)
	}
	return filepath.ToSlash(rel), nil
}

func digestJSON(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// fingerprint reads the counted bytes without following files outside the target.
func fingerprint(ctx context.Context, dir string, files []ScannedFile) (string, error) {
	entries := make([]ConfigSource, 0, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		path := filepath.Join(dir, filepath.FromSlash(f.Location))
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", camperrors.Wrapf(err, "resolving scanned file %s", f.Location)
		}
		if _, err := scanRelativePath(dir, real); err != nil {
			return "", err
		}
		info, err := os.Stat(real)
		if err != nil {
			return "", camperrors.Wrap(err, "reading scanned file")
		}
		if !info.Mode().IsRegular() {
			return "", camperrors.Newf("scanned path is not a regular file: %s", f.Location)
		}
		hash, err := hashFile(real)
		if err != nil {
			return "", err
		}
		entries = append(entries, ConfigSource{Path: f.Location, SHA256: hash})
	}
	return digestJSON(entries), nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", camperrors.Wrapf(err, "opening %s", path)
	}
	defer func() { _ = file.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", camperrors.Wrap(err, "hashing source")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
