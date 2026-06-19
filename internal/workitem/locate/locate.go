// Package locate resolves a workitem's on-disk location from a working
// directory under workflow/<type>/<slug>/, including dungeon layouts.
package locate

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/dungeon/statuspath"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

type Location struct {
	Type        string
	Slug        string
	ParentPath  string
	SourcePath  string
	DungeonPath string
	InDungeon   bool
	Status      string
}

func DetectFromCwd(campaignRoot, cwd string) (*Location, error) {
	if resolved, rerr := filepath.EvalSymlinks(campaignRoot); rerr == nil {
		campaignRoot = resolved
	}
	if resolved, rerr := filepath.EvalSymlinks(cwd); rerr == nil {
		cwd = resolved
	}
	rel, err := filepath.Rel(campaignRoot, cwd)
	if err != nil {
		return nil, camperrors.Wrap(err, "resolving cwd relative to campaign root")
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, camperrors.New(fmt.Sprintf("cwd %q is not under campaign root %q", cwd, campaignRoot))
	}
	if rel == "." {
		return nil, camperrors.New("not inside a workitem; cwd is at the campaign root")
	}

	parts := strings.Split(rel, "/")
	if parts[0] != "workflow" {
		return nil, camperrors.New("not inside a workitem; cwd must be under workflow/<type>/<slug>/")
	}
	if len(parts) < 3 {
		return nil, camperrors.New("not inside a workitem; cwd is at workflow root, expected workflow/<type>/<slug>/")
	}
	typeName := parts[1]
	if typeName == "" || typeName == "dungeon" {
		return nil, camperrors.New(fmt.Sprintf("not inside a workitem; %q is not a valid workflow type", typeName))
	}

	if parts[2] != "dungeon" {
		slug := parts[2]
		parentRel := filepath.Join("workflow", typeName)
		sourceRel := filepath.Join("workflow", typeName, slug)
		dungeonRel := filepath.Join("workflow", typeName, "dungeon")
		return &Location{
			Type:        typeName,
			Slug:        slug,
			ParentPath:  filepath.Join(campaignRoot, parentRel),
			SourcePath:  filepath.Join(campaignRoot, sourceRel),
			DungeonPath: filepath.Join(campaignRoot, dungeonRel),
		}, nil
	}

	if len(parts) < 5 {
		return nil, camperrors.New(fmt.Sprintf("not inside a workitem; cwd is at workflow/%s/dungeon[/...] without a slug", typeName))
	}
	status := parts[3]
	if status == "" {
		return nil, camperrors.New("not inside a workitem; cwd is at the dungeon root")
	}

	var slug string
	var parentRel string
	if statuspath.IsDateDir(parts[4]) {
		if len(parts) < 6 || parts[5] == "" {
			return nil, camperrors.New(fmt.Sprintf("not inside a workitem; cwd is at workflow/%s/dungeon/%s/%s without a slug", typeName, status, parts[4]))
		}
		slug = parts[5]
		parentRel = filepath.Join("workflow", typeName, "dungeon", status, parts[4])
	} else {
		slug = parts[4]
		parentRel = filepath.Join("workflow", typeName, "dungeon", status)
	}

	dungeonRel := filepath.Join("workflow", typeName, "dungeon")
	return &Location{
		Type:        typeName,
		Slug:        slug,
		ParentPath:  filepath.Join(campaignRoot, parentRel),
		SourcePath:  filepath.Join(campaignRoot, parentRel, slug),
		DungeonPath: filepath.Join(campaignRoot, dungeonRel),
		InDungeon:   true,
		Status:      status,
	}, nil
}
