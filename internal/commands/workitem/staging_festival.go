package workitem

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func inferFestivalIDFromCwd(campaignRoot, cwd string) string {
	rel := festivalRootRelFromCwd(campaignRoot, cwd)
	if rel == "" {
		return ""
	}
	if id := festivalMetadataID(campaignRoot, rel); id != "" {
		return id
	}
	return festivalRefFromString(rel)
}

func festivalRootRelFromCwd(campaignRoot, cwd string) string {
	if cwd == "" {
		got, err := osGetwd()
		if err != nil {
			return ""
		}
		cwd = got
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return ""
	}
	absRoot, err := filepath.Abs(campaignRoot)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(absCwd); err == nil {
		absCwd = resolved
	}
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}
	rel, err := filepath.Rel(absRoot, absCwd)
	if err != nil || relOutsideCampaignRoot(rel) {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 || parts[0] != "festivals" {
		return ""
	}
	switch parts[1] {
	case "planning", "ready", "active", "ritual", "chains":
		return strings.Join(parts[:3], "/")
	case "dungeon", ".dungeon":
		if len(parts) >= 5 && parts[2] == "completed" {
			return strings.Join(parts[:5], "/")
		}
		if len(parts) >= 4 {
			return strings.Join(parts[:4], "/")
		}
	}
	return ""
}

func festivalMetadataID(campaignRoot, rel string) string {
	data, err := os.ReadFile(filepath.Join(campaignRoot, filepath.FromSlash(rel), "fest.yaml"))
	if err != nil {
		return ""
	}
	var doc struct {
		Metadata struct {
			ID string `yaml:"id"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return ""
	}
	return festivalRefFromString(doc.Metadata.ID)
}

func festivalRefFromString(value string) string {
	return wkitem.FestivalRefFromID(value)
}

func relOutsideCampaignRoot(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}
