package nav

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"gopkg.in/yaml.v3"
)

var festivalIDPattern = regexp.MustCompile(`^(RI-)?[A-Z]{2}[0-9]{4,}$`)

// ResolveFestivalID resolves a canonical metadata identifier across festival
// lifecycle stages. It returns an empty path when query is not an ID or no
// festival matches. Reading metadata directly keeps moves and ID edits visible
// without waiting for the navigation index cache to expire.
func ResolveFestivalID(ctx context.Context, campaignRoot, query string) (string, error) {
	id := strings.ToUpper(query)
	if !festivalIDPattern.MatchString(id) {
		return "", nil
	}

	root := filepath.Join(campaignRoot, CategoryFestivals.Dir())
	stages := append([]string{""}, FestivalStatusDirs...)
	stages = append(stages, filepath.Join(".dungeon", "completed"),
		filepath.Join(".dungeon", "archived"), filepath.Join(".dungeon", "someday"))
	var matches []string
	for _, stage := range stages {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		stagePath := filepath.Join(root, stage)
		entries, err := os.ReadDir(stagePath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", camperrors.Wrapf(err, "reading festival stage %s", stagePath)
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			path := filepath.Join(stagePath, entry.Name())
			if !entry.IsDir() {
				if entry.Type()&os.ModeSymlink == 0 {
					continue
				}
				info, err := os.Stat(path)
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					return "", camperrors.Wrapf(err, "reading festival directory %s", path)
				}
				if !info.IsDir() {
					continue
				}
			}
			// A camp-owned lifecycle resident is not a festival, even if a
			// stale fest.yaml remains beside its .workitem marker.
			if _, err := os.Stat(filepath.Join(path, ".workitem")); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return "", camperrors.Wrapf(err, "reading festival ownership marker %s", path)
			}
			data, err := os.ReadFile(filepath.Join(path, "fest.yaml"))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return "", camperrors.Wrapf(err, "reading festival metadata %s", path)
			}
			var metadata struct {
				Metadata struct {
					ID string `yaml:"id"`
				} `yaml:"metadata"`
			}
			if err := yaml.Unmarshal(data, &metadata); err != nil || metadata.Metadata.ID != id {
				continue
			}
			matches = append(matches, path)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	if len(matches) > 1 {
		return "", camperrors.Newf("festival ID %s is ambiguous: %s", id, strings.Join(matches, ", "))
	}
	return matches[0], nil
}
