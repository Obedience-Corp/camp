package skills

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
)

// campAppRunner reaches the camp-app plugin on PATH. camp asks it only for a
// plugin skill bundle a request names.
var campAppRunner = intskills.PluginRunner("app")

// planSources is what planning a named request reads: the camp's manifest and
// camp-app. Tests replace both.
type planSources struct {
	runner   intskills.ProviderRunner
	manifest func(root string) (*intskills.Manifest, error)
}

func campPlanSources() planSources {
	return planSources{runner: campAppRunner, manifest: readCampManifest}
}

func readCampManifest(root string) (*intskills.Manifest, error) {
	return intskills.ReadManifest(os.DirFS(filepath.Join(root, campaign.CampaignDir, intskills.SkillsSubdir)))
}

// unresolvedBundle is a named plugin bundle the camp records that camp-app
// could not refresh in this run.
type unresolvedBundle struct {
	slug    string
	plugin  string
	state   intskills.ShippedState
	version string
	// notOffered is set when camp-app answered but no longer offers it.
	notOffered bool
}

// pluginPlan splits a named request into camp's bundles and plugin bundles.
type pluginPlan struct {
	// updateSlugs are the bundles to pass to Update; empty with named args
	// means there is nothing to update, never every camp bundle.
	updateSlugs []string
	bundles     []intskills.PluginBundle
	report      *intskills.ProviderReport
	unresolved  []unresolvedBundle
}

// planPluginSlugs asks camp-app only when a named slug is not one camp ships.
// A person's request fails before anything is written when a slug it names
// cannot be resolved and is not recorded in the camp; an automatic request
// (auto) never fails for camp-app's sake and reports it under providers.
func planPluginSlugs(ctx context.Context, src planSources, root string, shipped []intskills.ShippedFile, args []string, auto bool) (pluginPlan, error) {
	var plan pluginPlan
	campSlugs := intskills.ShippedSlugs(shipped)
	var named []string
	for _, slug := range args {
		switch {
		case slices.Contains(campSlugs, slug):
			if !slices.Contains(plan.updateSlugs, slug) {
				plan.updateSlugs = append(plan.updateSlugs, slug)
			}
		case !slices.Contains(named, slug):
			named = append(named, slug)
		}
	}
	if len(named) == 0 {
		return plan, nil
	}
	// A slug that cannot be a bundle directory is refused before camp reads
	// anything or asks camp-app, so it can never reach a path.
	if bad := slices.DeleteFunc(slices.Clone(named), intskills.ValidPluginBundleName); len(bad) > 0 {
		return plan, intskills.UnknownBundleError(bad, campSlugs)
	}
	manifest, err := src.manifest(root)
	if err != nil {
		return plan, err
	}
	report, err := intskills.AskProvider(ctx, intskills.CampAppPlugin, src.runner, campSlugs)
	if err != nil {
		return plan, err
	}
	plan.report = &report
	unknown := plan.resolve(named, manifest, auto)
	if len(unknown) > 0 {
		return plan, unknownPluginBundleError(unknown, campSlugs, report)
	}
	return plan, nil
}

func (plan *pluginPlan) resolve(named []string, manifest *intskills.Manifest, auto bool) []string {
	var unknown []string
	for _, slug := range named {
		if bundle, ok := plan.report.Offered(slug); ok {
			plan.bundles = append(plan.bundles, bundle)
			plan.updateSlugs = append(plan.updateSlugs, slug)
			continue
		}
		var rec intskills.ProviderRecord
		recorded := false
		if manifest != nil {
			rec, recorded = manifest.Providers[slug]
		}
		switch {
		case recorded:
			plan.unresolved = append(plan.unresolved, plan.unresolvedFor(slug, rec))
		case !auto:
			unknown = append(unknown, slug)
		}
	}
	return unknown
}

// unresolvedFor is the row for a recorded bundle camp-app did not serve. When
// camp-app answered and simply no longer offers it, the provider's detail
// says so, to tell it apart from camp-app being unavailable.
func (plan *pluginPlan) unresolvedFor(slug string, rec intskills.ProviderRecord) unresolvedBundle {
	u := unresolvedBundle{slug: slug, plugin: cmp.Or(rec.Plugin, intskills.CampAppPlugin), state: intskills.ShippedProviderMissing, version: rec.Version}
	if rec.Declined {
		u.state = intskills.ShippedDeclined
	}
	if plan.report.Status == intskills.ProviderOK {
		u.notOffered = true
		note := plan.report.Plugin + " no longer offers " + slug
		plan.report.Detail = strings.TrimPrefix(plan.report.Detail+"; "+note, "; ")
	}
	return u
}

// unknownPluginBundleError keeps today's refusal, byte for byte, when camp-app
// is not installed, and otherwise says what camp-app answered.
func unknownPluginBundleError(unknown, campSlugs []string, report intskills.ProviderReport) error {
	base := intskills.UnknownBundleError(unknown, campSlugs)
	switch report.Status {
	case intskills.ProviderNotInstalled:
		return base
	case intskills.ProviderOK:
		offered := "none"
		if names := report.OfferedNames(); len(names) > 0 {
			offered = strings.Join(names, ", ")
		}
		return camperrors.Newf("%s; %s offers: %s", base.Error(), report.Plugin, offered)
	default:
		return camperrors.Newf("%s; %s", base.Error(), report.SkipLine())
	}
}

// updateScope is the scope Update gets and whether it runs at all. With no
// slug it refreshes every camp bundle (nil slugs). With slugs it runs only on
// those that resolved: Update reads an empty Slugs as every camp bundle, so a
// request naming only bundles that did not resolve must not reach it.
func updateScope(args []string, plan pluginPlan) ([]string, bool) {
	if len(args) == 0 {
		return nil, true
	}
	return plan.updateSlugs, len(plan.updateSlugs) > 0
}

// linkSlugs are the named bundles, or every bundle in scope when none were
// named, that are present and not declined after the run. Under a dry run a
// bundle the run would write counts as present, so the reported links are
// the ones a real run would make.
func linkSlugs(skillsDir string, args []string, res *intskills.UpdateResult, plan pluginPlan) []string {
	candidates := args
	if len(candidates) == 0 {
		for _, f := range res.Files {
			if !slices.Contains(candidates, f.Bundle) {
				candidates = append(candidates, f.Bundle)
			}
		}
	}
	declined := make(map[string]bool)
	wouldWrite := make(map[string]bool)
	for _, f := range res.Files {
		declined[f.Bundle] = declined[f.Bundle] || f.State == intskills.ShippedDeclined
		wouldWrite[f.Bundle] = wouldWrite[f.Bundle] || (res.DryRun && f.Action.Writes())
	}
	for _, u := range plan.unresolved {
		declined[u.slug] = declined[u.slug] || u.state == intskills.ShippedDeclined
	}
	var slugs []string
	for _, slug := range candidates {
		info, err := os.Stat(filepath.Join(skillsDir, slug, "SKILL.md"))
		present := wouldWrite[slug] || (err == nil && info.Mode().IsRegular())
		if present && !declined[slug] && !slices.Contains(slugs, slug) {
			slugs = append(slugs, slug)
		}
	}
	return slugs
}
