package skills

import (
	"io"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// LinkResult reports the projection outcome for a single tool.
type LinkResult struct {
	Tool    string
	Dest    string
	Summary ProjectionSummary
	Err     error
}

// LinkDefaultTools projects every skill bundle in skillsDir into every tool in
// the registry, rooted at campaignRoot. It never aborts on a single tool's
// failure: per-tool errors are captured in the returned LinkResult slice. A
// top-level error is returned only when the skills directory cannot be read.
//
// force only affects user-placed foreign symlinks; camp-managed links are healed
// regardless and real files/directories are never overwritten (reported as
// conflicts in the per-tool summary).
func LinkDefaultTools(campaignRoot, skillsDir string, dryRun, force bool, errOut io.Writer) ([]LinkResult, error) {
	slugs, err := DiscoverSkillSlugs(skillsDir)
	if err != nil {
		return nil, camperrors.Wrap(err, "discover skill bundles")
	}

	tools := ToolNames()
	results := make([]LinkResult, 0, len(tools))
	for _, tool := range tools {
		res := LinkResult{Tool: tool}

		relPath, err := ResolveToolPath(tool)
		if err != nil {
			res.Err = err
			results = append(results, res)
			continue
		}
		dest := filepath.Join(campaignRoot, relPath)
		res.Dest = dest

		if err := ValidateDestination(dest, campaignRoot); err != nil {
			res.Err = err
			results = append(results, res)
			continue
		}
		if len(slugs) == 0 {
			results = append(results, res)
			continue
		}
		if err := EnsureProjectionDirectory(dest, dryRun, errOut); err != nil {
			res.Err = err
			results = append(results, res)
			continue
		}

		summary, err := ProjectSkillEntries(dest, skillsDir, slugs, dryRun, force)
		res.Summary = summary
		res.Err = err
		results = append(results, res)
	}

	return results, nil
}
