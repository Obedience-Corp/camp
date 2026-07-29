// Package locate resolves a workitem's on-disk location from a working
// directory under workflow/<type>/<slug>/ or, for a workitem promoted onto the
// festival rail, under festivals/<stage>/<slug>/, including dungeon layouts.
package locate

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	"github.com/Obedience-Corp/camp/internal/dungeon/statuspath"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// festivalsRoot is the campaign-relative folder holding the festival lifecycle
// stages. A workitem promoted onto the festival rail physically lives here
// instead of under workflow/<type>/.
const festivalsRoot = "festivals"

// festivalDungeonName is the festival-local dungeon. Unlike a workflow type's
// dungeon, whose on-disk spelling is resolved with spelling.Resolve, this one is
// the fixed hidden .dungeon, so a resident always completes into
// festivals/.dungeon/<status>/<date>/<slug> rather than into the dungeon of the
// workflow type it came from.
const festivalDungeonName = ".dungeon"

// festivalStages are the rail stages a resident workitem can occupy, named from
// the lifecycle vocabulary so the folder this resolver accepts cannot drift from
// the stage a workitem can be promoted to. Promotion onto the rail targets ready
// and active only; festivals/ also holds folders that are not rail stages
// (planning, chains, ritual), and those do not resolve.
var festivalStages = map[string]bool{
	string(wkitem.LifecycleStageReady):  true,
	string(wkitem.LifecycleStageActive): true,
}

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
	switch parts[0] {
	case "workflow":
		return detectWorkflowItem(campaignRoot, parts)
	case festivalsRoot:
		return detectFestivalResident(campaignRoot, parts)
	default:
		return nil, camperrors.New("not inside a workitem; cwd must be under workflow/<type>/<slug>/")
	}
}

// detectWorkflowItem resolves the workflow/<type>/... layouts. The type comes
// from the path segment, and the dungeon is that type's own dungeon folder in
// whichever spelling exists on disk.
func detectWorkflowItem(campaignRoot string, parts []string) (*Location, error) {
	if len(parts) < 3 {
		return nil, camperrors.New("not inside a workitem; cwd is at workflow root, expected workflow/<type>/<slug>/")
	}
	typeName := parts[1]
	if typeName == "" || spelling.IsDungeonName(typeName) {
		return nil, camperrors.New(fmt.Sprintf("not inside a workitem; %q is not a valid workflow type", typeName))
	}
	if !spelling.IsDungeonName(parts[2]) {
		return workflowActive(campaignRoot, typeName, parts[2])
	}
	return workflowDungeon(campaignRoot, typeName, parts)
}

func workflowActive(campaignRoot, typeName, slug string) (*Location, error) {
	parentRel := filepath.Join("workflow", typeName)
	sourceRel := filepath.Join("workflow", typeName, slug)
	resolved, err := spelling.Resolve(context.Background(), filepath.Join(campaignRoot, parentRel))
	if err != nil {
		return nil, err
	}
	dungeonRel := filepath.Join("workflow", typeName, resolved.Name)
	return &Location{
		Type:        typeName,
		Slug:        slug,
		ParentPath:  filepath.Join(campaignRoot, parentRel),
		SourcePath:  filepath.Join(campaignRoot, sourceRel),
		DungeonPath: filepath.Join(campaignRoot, dungeonRel),
	}, nil
}

func workflowDungeon(campaignRoot, typeName string, parts []string) (*Location, error) {
	dungeonName := parts[2]
	if len(parts) < 5 {
		return nil, camperrors.New(fmt.Sprintf("not inside a workitem; cwd is at workflow/%s/%s[/...] without a slug", typeName, dungeonName))
	}
	status := parts[3]
	if status == "" {
		return nil, camperrors.New("not inside a workitem; cwd is at the dungeon root")
	}

	var slug string
	var parentRel string
	if statuspath.IsDateDir(parts[4]) {
		if len(parts) < 6 || parts[5] == "" {
			return nil, camperrors.New(fmt.Sprintf("not inside a workitem; cwd is at workflow/%s/%s/%s/%s without a slug", typeName, dungeonName, status, parts[4]))
		}
		slug = parts[5]
		parentRel = filepath.Join("workflow", typeName, dungeonName, status, parts[4])
	} else {
		slug = parts[4]
		parentRel = filepath.Join("workflow", typeName, dungeonName, status)
	}

	dungeonRel := filepath.Join("workflow", typeName, dungeonName)
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

// detectFestivalResident resolves a workitem living on the festival rail, either
// at a working stage (festivals/ready|active/<slug>) or already shelved
// (festivals/.dungeon/<status>[/<date>]/<slug>). Both shapes report
// DungeonPath = festivals/.dungeon so the shared move plumbing keeps a resident
// inside the festival tree instead of sending it back to its workflow type.
func detectFestivalResident(campaignRoot string, parts []string) (*Location, error) {
	if len(parts) < 2 || parts[1] == "" {
		return nil, camperrors.New("not inside a workitem; cwd is at the festivals root")
	}
	if parts[1] == festivalDungeonName {
		return festivalDungeon(campaignRoot, parts)
	}
	return festivalStage(campaignRoot, parts)
}

func festivalStage(campaignRoot string, parts []string) (*Location, error) {
	stage := parts[1]
	if !festivalStages[stage] {
		return nil, camperrors.New(fmt.Sprintf("not inside a workitem; festivals/%s is not a rail stage (expected festivals/ready/<slug> or festivals/active/<slug>)", stage))
	}
	if len(parts) < 3 || parts[2] == "" {
		return nil, camperrors.New(fmt.Sprintf("not inside a workitem; cwd is at festivals/%s without a slug", stage))
	}

	slug := parts[2]
	parentRel := festivalsRoot + "/" + stage
	sourceRel := parentRel + "/" + slug
	typeName, err := residentType(filepath.Join(campaignRoot, sourceRel), sourceRel)
	if err != nil {
		return nil, err
	}
	return &Location{
		Type:        typeName,
		Slug:        slug,
		ParentPath:  filepath.Join(campaignRoot, parentRel),
		SourcePath:  filepath.Join(campaignRoot, sourceRel),
		DungeonPath: filepath.Join(campaignRoot, festivalsRoot, festivalDungeonName),
	}, nil
}

func festivalDungeon(campaignRoot string, parts []string) (*Location, error) {
	if len(parts) < 4 || parts[2] == "" {
		return nil, camperrors.New("not inside a workitem; cwd is at the festivals dungeon root")
	}
	status := parts[2]

	var slug string
	var parentRel string
	if statuspath.IsDateDir(parts[3]) {
		if len(parts) < 5 || parts[4] == "" {
			return nil, camperrors.New(fmt.Sprintf("not inside a workitem; cwd is at festivals/%s/%s/%s without a slug", festivalDungeonName, status, parts[3]))
		}
		slug = parts[4]
		parentRel = festivalsRoot + "/" + festivalDungeonName + "/" + status + "/" + parts[3]
	} else {
		slug = parts[3]
		parentRel = festivalsRoot + "/" + festivalDungeonName + "/" + status
	}

	sourceRel := parentRel + "/" + slug
	typeName, err := residentType(filepath.Join(campaignRoot, sourceRel), sourceRel)
	if err != nil {
		return nil, err
	}
	return &Location{
		Type:        typeName,
		Slug:        slug,
		ParentPath:  filepath.Join(campaignRoot, parentRel),
		SourcePath:  filepath.Join(campaignRoot, sourceRel),
		DungeonPath: filepath.Join(campaignRoot, festivalsRoot, festivalDungeonName),
		InDungeon:   true,
		Status:      status,
	}, nil
}

// residentType reads a resident's workflow type from its .workitem marker. The
// path cannot supply it the way workflow/<type>/ does, because a resident's
// parent segment is a lifecycle stage rather than a type. An unstamped directory
// in a lifecycle folder is not a resident camp can move, so it is a clear error
// rather than a guess; camp workitem doctor surfaces these.
func residentType(absDir, relPath string) (string, error) {
	meta, err := wkitem.LoadMetadata(context.Background(), absDir)
	if err != nil {
		return "", err
	}
	if meta == nil {
		return "", camperrors.New(fmt.Sprintf("not a lifecycle resident; %s has no .workitem marker (run camp workitem doctor)", relPath))
	}
	return meta.Type, nil
}
