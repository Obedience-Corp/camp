package stageguard

import (
	"context"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Shipped defaults, applied when config is silent. The campaign figure is low
// because campaign content is overwhelmingly text, so the exceptions are heavy
// reference material an artifact root should own. The project figure is higher
// because the outcome there is a stop rather than a silent fix.
const (
	// DefaultMaxFileSize is the campaign-root per-file threshold.
	DefaultMaxFileSize int64 = 10 << 20 // 10 MiB
	// DefaultProjectMaxFileSize is the per-file threshold inside a project.
	DefaultProjectMaxFileSize int64 = 25 << 20 // 25 MiB
	// DefaultMaxAddedFiles is the bulk trigger count.
	DefaultMaxAddedFiles = 1000
)

// ResolveLimits loads the guard policy that applies to repoPath. The stage
// chokepoint calls it with no config knowledge of its own, which is what lets
// fest inherit the guard from a version bump with no fest code change.
//
// Outside a campaign there is no campaign.yaml, so the project-scope defaults
// apply: a bare git repo is closer to a project than to a campaign root.
func ResolveLimits(ctx context.Context, repoPath string) (GuardLimits, error) {
	if ctx.Err() != nil {
		return GuardLimits{}, ctx.Err()
	}

	campaignRoot, err := config.DetectCampaignRoot(ctx, repoPath)
	if err != nil {
		if camperrors.Is(err, config.ErrNotInCampaign) {
			return defaultLimits(true), nil
		}
		return GuardLimits{}, camperrors.Wrapf(err, "resolve campaign root for %s", repoPath)
	}

	cfg, err := config.LoadCampaignConfig(ctx, campaignRoot)
	if err != nil {
		return GuardLimits{}, camperrors.Wrapf(err, "load campaign config at %s", campaignRoot)
	}

	paths := config.DefaultCampaignPaths()
	if cfg != nil && cfg.Jumps != nil {
		paths = cfg.Jumps.Paths
	}
	scopeProject, projectName := resolveScope(campaignRoot, repoPath, paths)
	if cfg == nil {
		return defaultLimits(scopeProject), nil
	}
	return applyGuardsConfig(cfg.Commit.Guards, scopeProject, projectName)
}

// defaultLimits returns the shipped policy for a scope.
func defaultLimits(scopeProject bool) GuardLimits {
	maxFileSize := DefaultMaxFileSize
	if scopeProject {
		maxFileSize = DefaultProjectMaxFileSize
	}
	return GuardLimits{
		MaxFileSize:   maxFileSize,
		MaxAddedFiles: DefaultMaxAddedFiles,
		LargeFiles:    ModeAuto,
		Bulk:          ModeBlock,
		ScopeProject:  scopeProject,
	}
}

// applyGuardsConfig overlays configured values onto the shipped defaults for a
// scope. An unset field keeps its default; an invalid one is an error rather
// than a silent fallback, so a typo never quietly disables a guard.
func applyGuardsConfig(g config.GuardsConfig, scopeProject bool, projectName string) (GuardLimits, error) {
	limits := defaultLimits(scopeProject)
	limits.Allow = g.ResolveAllow(projectName)

	if raw := g.ResolveMaxFileSize(projectName); raw != "" {
		size, err := ParseByteSize(raw)
		if err != nil {
			return GuardLimits{}, camperrors.NewValidation("commit.guards.max_file_size", err.Error(), err)
		}
		limits.MaxFileSize = size
	}
	if n := g.ResolveMaxAddedFiles(projectName); n > 0 {
		limits.MaxAddedFiles = n
	}
	if raw := g.ResolveGuardMode(projectName); raw != "" {
		mode := Mode(raw)
		if !mode.Valid() {
			return GuardLimits{}, camperrors.NewValidation("commit.guards.large_files",
				"must be auto, block, or off, got "+strconv.Quote(raw), nil)
		}
		limits.LargeFiles = mode
	}
	if raw := g.ResolveBulkMode(projectName); raw != "" {
		mode := Mode(raw)
		// No auto: camp cannot infer whether a bulk tree wants gitignore or an
		// artifact root, so offering the mode would promise a decision camp
		// has no basis to make.
		if mode != ModeBlock && mode != ModeOff {
			return GuardLimits{}, camperrors.NewValidation("commit.guards.bulk",
				"must be block or off, got "+strconv.Quote(raw), nil)
		}
		limits.Bulk = mode
	}
	return limits, nil
}

// resolveScope reports whether repoPath is a project rather than the campaign
// root, and the project name used to key per-project overrides.
//
// The projects and worktrees directories are campaign-configurable
// (.campaign/settings/jumps.yaml), so they are read from paths rather than
// assumed. Worktrees is checked first because it nests under projects by
// default, and a worktree keys to the project it belongs to so a per-project
// override applies to work done in either place.
func resolveScope(campaignRoot, repoPath string, paths config.CampaignPaths) (scopeProject bool, projectName string) {
	rootAbs, err := filepath.Abs(campaignRoot)
	if err != nil {
		rootAbs = campaignRoot
	}
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		repoAbs = repoPath
	}

	rel, err := filepath.Rel(rootAbs, repoAbs)
	if err != nil || strings.HasPrefix(rel, "..") {
		// Outside the campaign tree entirely: treat as a bare project.
		return true, ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return false, ""
	}

	for _, container := range []string{paths.Worktrees, paths.Projects} {
		if name, ok := firstSegmentUnder(rel, container); ok {
			return true, name
		}
	}
	segments := strings.Split(rel, "/")
	return true, segments[len(segments)-1]
}

// firstSegmentUnder returns the first path segment of rel below container, if
// rel lives there. An empty or "." container never matches, so an unconfigured
// path cannot swallow the whole campaign.
func firstSegmentUnder(rel, container string) (string, bool) {
	container = strings.Trim(strings.TrimSpace(filepath.ToSlash(container)), "/")
	if container == "" || container == "." {
		return "", false
	}
	if !strings.HasPrefix(rel, container+"/") {
		return "", false
	}
	remainder := strings.TrimPrefix(rel, container+"/")
	segments := strings.Split(remainder, "/")
	if segments[0] == "" {
		return "", false
	}
	return segments[0], true
}

// byteUnits maps a size suffix to its multiplier. Binary (MiB) and decimal
// (MB) units are both accepted because users write both and the difference at
// these magnitudes never changes which side of a threshold a file lands on.
var byteUnits = []struct {
	suffix string
	factor int64
}{
	{"KIB", 1 << 10}, {"MIB", 1 << 20}, {"GIB", 1 << 30}, {"TIB", 1 << 40},
	{"KB", 1000}, {"MB", 1000 * 1000}, {"GB", 1000 * 1000 * 1000}, {"TB", 1000 * 1000 * 1000 * 1000},
	{"K", 1 << 10}, {"M", 1 << 20}, {"G", 1 << 30}, {"T", 1 << 40},
	{"B", 1},
}

// ParseByteSize parses a human byte size such as "10MiB", "50MB", or "1024".
// A bare number is bytes. Parsing is case-insensitive and tolerates a space
// before the unit.
func ParseByteSize(raw string) (int64, error) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return 0, camperrors.New("empty byte size")
	}

	for _, unit := range byteUnits {
		if !strings.HasSuffix(s, unit.suffix) {
			continue
		}
		digits := strings.TrimSpace(strings.TrimSuffix(s, unit.suffix))
		value, err := parseSizeNumber(digits, raw)
		if err != nil {
			return 0, err
		}
		if value > maxSizeValue/unit.factor {
			return 0, camperrors.Newf("byte size %q overflows int64", raw)
		}
		return value * unit.factor, nil
	}
	return parseSizeNumber(s, raw)
}

const maxSizeValue = int64(math.MaxInt64)

func parseSizeNumber(digits, raw string) (int64, error) {
	if digits == "" {
		return 0, camperrors.Newf("byte size %q has no number", raw)
	}
	value, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, camperrors.Wrapf(err, "parse byte size %q", raw)
	}
	if value < 0 {
		return 0, camperrors.Newf("byte size %q must not be negative", raw)
	}
	return value, nil
}
