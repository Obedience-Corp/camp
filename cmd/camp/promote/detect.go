package promote

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/dungeon/statuspath"
)

type WorkitemLocation struct {
	Type        string
	Slug        string
	ParentPath  string
	SourcePath  string
	DungeonPath string
	InDungeon   bool
	Status      string
}

func detectWorkitemFromCwd(campaignRoot, cwd string) (*WorkitemLocation, error) {
	rel, err := filepath.Rel(campaignRoot, cwd)
	if err != nil {
		return nil, fmt.Errorf("resolving cwd relative to campaign root: %w", err)
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, fmt.Errorf("cwd %q is not under campaign root %q", cwd, campaignRoot)
	}
	if rel == "." {
		return nil, fmt.Errorf("not inside a workitem; cwd is at the campaign root")
	}

	parts := strings.Split(rel, "/")
	if parts[0] != "workflow" {
		return nil, fmt.Errorf("not inside a workitem; cwd must be under workflow/<type>/<slug>/")
	}
	if len(parts) < 3 {
		return nil, fmt.Errorf("not inside a workitem; cwd is at workflow root, expected workflow/<type>/<slug>/")
	}
	typeName := parts[1]
	if typeName == "" || typeName == "dungeon" {
		return nil, fmt.Errorf("not inside a workitem; %q is not a valid workflow type", typeName)
	}

	if parts[2] != "dungeon" {
		slug := parts[2]
		parentRel := filepath.Join("workflow", typeName)
		sourceRel := filepath.Join("workflow", typeName, slug)
		dungeonRel := filepath.Join("workflow", typeName, "dungeon")
		return &WorkitemLocation{
			Type:        typeName,
			Slug:        slug,
			ParentPath:  filepath.Join(campaignRoot, parentRel),
			SourcePath:  filepath.Join(campaignRoot, sourceRel),
			DungeonPath: filepath.Join(campaignRoot, dungeonRel),
		}, nil
	}

	if len(parts) < 5 {
		return nil, fmt.Errorf("not inside a workitem; cwd is at workflow/%s/dungeon[/...] without a slug", typeName)
	}
	status := parts[3]
	if status == "" {
		return nil, fmt.Errorf("not inside a workitem; cwd is at the dungeon root")
	}

	var slug string
	var parentRel string
	if statuspath.IsDateDir(parts[4]) {
		if len(parts) < 6 || parts[5] == "" {
			return nil, fmt.Errorf("not inside a workitem; cwd is at workflow/%s/dungeon/%s/%s without a slug", typeName, status, parts[4])
		}
		slug = parts[5]
		parentRel = filepath.Join("workflow", typeName, "dungeon", status, parts[4])
	} else {
		slug = parts[4]
		parentRel = filepath.Join("workflow", typeName, "dungeon", status)
	}

	dungeonRel := filepath.Join("workflow", typeName, "dungeon")
	return &WorkitemLocation{
		Type:        typeName,
		Slug:        slug,
		ParentPath:  filepath.Join(campaignRoot, parentRel),
		SourcePath:  filepath.Join(campaignRoot, parentRel, slug),
		DungeonPath: filepath.Join(campaignRoot, dungeonRel),
		InDungeon:   true,
		Status:      status,
	}, nil
}
