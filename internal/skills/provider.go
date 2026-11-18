package skills

import (
	"context"
	"errors"
	"strconv"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/plugin"
)

// CampAppPlugin is the one plugin camp asks for skill bundles in this version.
const CampAppPlugin = "camp-app"

// ProviderStatus is how a plugin answered a request for its skill bundles.
type ProviderStatus string

const (
	// ProviderOK means the plugin offered a valid skills document.
	ProviderOK ProviderStatus = "ok"
	// ProviderNotInstalled means the plugin is not on PATH.
	ProviderNotInstalled ProviderStatus = "not-installed"
	// ProviderNoContract means the plugin offers no skills document this camp
	// reads: it predates the contract, or offers a newer one.
	ProviderNoContract ProviderStatus = "no-contract"
	// ProviderInvalid means the answer could not be used.
	ProviderInvalid ProviderStatus = "invalid"
	// ProviderTimedOut means the plugin did not answer in time.
	ProviderTimedOut ProviderStatus = "timed-out"
)

// ErrProviderNotInstalled is what a ProviderRunner returns when the plugin
// binary cannot be found.
var ErrProviderNotInstalled = camperrors.New("plugin not installed")

// ProviderRunner runs the plugin with args and returns its stdout. A non-zero
// exit returns the stdout with an error.
type ProviderRunner func(ctx context.Context, args []string) ([]byte, error)

// ProviderReport is what camp learned from asking one plugin for its skills.
type ProviderReport struct {
	Plugin  string
	Status  ProviderStatus
	Version string
	// Detail says why the status is not ok.
	Detail string
	// CampBehind marks a no-contract answer in a schema newer than this camp
	// reads, so the fix is to update camp, not the plugin.
	CampBehind bool
	Bundles    []PluginBundle
}

var (
	providerDeadline           = 5 * time.Second
	providerStdoutLimit  int64 = 2 << 20
	providerSkillCommand       = []string{"skill", "--json"}
)

// PluginRunner returns the runner for the plugin binary camp-<name> on PATH.
func PluginRunner(name string) ProviderRunner {
	return func(ctx context.Context, args []string) ([]byte, error) {
		p, found := plugin.Lookup(name)
		if !found {
			return nil, ErrProviderNotInstalled
		}
		return plugin.Output(ctx, p, args, providerStdoutLimit)
	}
}

// AskProvider asks the plugin for its skill bundles, bounded by a deadline and
// an output cap. Every problem with the plugin is reported in the result; the
// only error is ctx ending, when the caller was interrupted.
func AskProvider(ctx context.Context, pluginName string, run ProviderRunner, campSlugs []string) (ProviderReport, error) {
	report := ProviderReport{Plugin: pluginName}
	callCtx, cancel := context.WithTimeout(ctx, providerDeadline)
	defer cancel()
	out, err := run(callCtx, providerSkillCommand)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return report, ctxErr
	}

	var cmdErr *camperrors.CommandError
	switch {
	case errors.Is(err, ErrProviderNotInstalled):
		report.Status, report.Detail = ProviderNotInstalled, pluginName+" is not installed"
		return report, nil
	case callCtx.Err() != nil:
		report.Status, report.Detail = ProviderTimedOut, "no answer within "+providerDeadline.String()
		return report, nil
	case errors.Is(err, plugin.ErrOutputTooLarge):
		report.Status, report.Detail = ProviderInvalid, "it printed more than "+strconv.FormatInt(providerStdoutLimit, 10)+" bytes"
		return report, nil
	case err != nil && !errors.As(err, &cmdErr):
		report.Status, report.Detail = ProviderInvalid, err.Error()
		return report, nil
	}
	return classifyAnswer(report, out, err != nil, campSlugs), nil
}

func classifyAnswer(report ProviderReport, out []byte, exited bool, campSlugs []string) ProviderReport {
	bundles, version, err := parsePluginAnswer(out, exited, report.Plugin, campSlugs)
	report.Version = version
	switch {
	case errors.Is(err, errMoreThanSkill):
		report.Status, report.CampBehind = ProviderNoContract, true
		report.Detail = "it offers a skill with more than SKILL.md, which this camp does not install; update camp"
	case errors.Is(err, errNewerContract):
		report.Status, report.CampBehind = ProviderNoContract, true
		report.Detail = "it offers a newer skill contract than this camp reads; update camp"
	case errors.Is(err, errNoContract):
		report.Status = ProviderNoContract
		report.Detail = "this " + report.Plugin + " does not offer skills to camp; update " + report.Plugin
	case err != nil:
		report.Status, report.Detail = ProviderInvalid, err.Error()
	default:
		report.Status, report.Bundles = ProviderOK, bundles
	}
	return report
}

// Offered returns the bundle the plugin offers under name.
func (r ProviderReport) Offered(name string) (PluginBundle, bool) {
	for _, b := range r.Bundles {
		if b.Name == name {
			return b, true
		}
	}
	return PluginBundle{}, false
}

// OfferedNames lists the bundle names the plugin offers.
func (r ProviderReport) OfferedNames() []string {
	names := make([]string, 0, len(r.Bundles))
	for _, b := range r.Bundles {
		names = append(names, b.Name)
	}
	return names
}

// SkipLine is the warning camp prints when the plugin's skills were skipped.
func (r ProviderReport) SkipLine() string {
	return "skipped skills from " + r.Plugin + ": " + r.Detail
}
