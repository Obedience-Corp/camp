package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	navfuzzy "github.com/Obedience-Corp/camp/internal/nav/fuzzy"
)

// errFleetMiss means no other machine has a camp for this query. Callers keep
// the local "not found" error in that case.
var errFleetMiss = errors.New("no matching camp on another machine")

// fleetCamp is one camp name known to live on a machine other than this one.
type fleetCamp struct {
	Machine string
	Name    string
	ID      string
	Org     string
	Status  string
}

// normalizeCampName folds case and drops the separators people leave out when
// they type a camp name, so "mytools" and "My_Tools" are the same camp.
func normalizeCampName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch r {
		case '_', '-', ' ':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sameCampaignName(a, b string) bool {
	na, nb := normalizeCampName(a), normalizeCampName(b)
	return na != "" && na == nb
}

// matchFleetCamps resolves query against camps on other machines.
// A separator-insensitive unique name wins. Otherwise the best fuzzy match
// wins when it is strictly better than the runner-up and at least a prefix-quality
// score, and that name lives on exactly one machine.
func matchFleetCamps(query string, camps []fleetCamp) (fleetCamp, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(camps) == 0 {
		return fleetCamp{}, errFleetMiss
	}
	camps = dedupeFleetCamps(camps)

	nq := normalizeCampName(query)
	var normHits []fleetCamp
	for _, c := range camps {
		if normalizeCampName(c.Name) == nq {
			normHits = append(normHits, c)
		}
	}
	if len(normHits) == 1 {
		return normHits[0], nil
	}
	if len(normHits) > 1 {
		return fleetCamp{}, ambiguousFleetError(query, normHits)
	}

	byName := map[string][]fleetCamp{}
	var names []string
	seen := map[string]struct{}{}
	for _, c := range camps {
		byName[c.Name] = append(byName[c.Name], c)
		if _, ok := seen[c.Name]; ok {
			continue
		}
		seen[c.Name] = struct{}{}
		names = append(names, c.Name)
	}
	matches := navfuzzy.Filter(names, query)
	if len(matches) == 0 || matches[0].Score < navfuzzy.ScorePrefixMatch {
		return fleetCamp{}, errFleetMiss
	}
	best := matches[0]
	if len(matches) > 1 && matches[1].Score == best.Score {
		var tied []fleetCamp
		for _, m := range matches {
			if m.Score != best.Score {
				break
			}
			tied = append(tied, byName[m.Target]...)
		}
		return fleetCamp{}, ambiguousFleetError(query, tied)
	}
	hits := byName[best.Target]
	if len(hits) != 1 {
		return fleetCamp{}, ambiguousFleetError(query, hits)
	}
	return hits[0], nil
}

func dedupeFleetCamps(camps []fleetCamp) []fleetCamp {
	seen := make(map[string]struct{}, len(camps))
	out := make([]fleetCamp, 0, len(camps))
	for _, c := range camps {
		if c.Machine == "" || c.Name == "" {
			continue
		}
		key := c.Machine + "\n" + c.Org + "\n" + c.Name + "\n" + c.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}
	return out
}

func ambiguousFleetError(query string, hits []fleetCamp) error {
	labels := make([]string, 0, len(hits))
	seen := map[string]struct{}{}
	for _, h := range hits {
		label := h.Machine + ":" + scopedFleetName(h.Org, h.Name)
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	sort.Strings(labels)
	shown := labels
	if len(shown) > 5 {
		shown = shown[:5]
	}
	return camperrors.New(fmt.Sprintf("camp %q matches more than one remote camp: %s\nHint: pass the machine, for example 'csw %s'",
		query, strings.Join(shown, ", "), shown[0]))
}

// Fleet selection needs authoritative org and lifecycle metadata. Completion
// caches contain names only (possibly from --all), so they cannot select a camp.
// The hop origin is enumerated like every other machine, even before adoption.
var fleetEnumerator = enumerateRemoteFor

func lookupFleetCampaign(ctx context.Context, query string, scope cmdutil.CampaignScope) (fleetCamp, error) {
	if err := ctx.Err(); err != nil {
		return fleetCamp{}, err
	}
	mf, err := machines.Load()
	if err != nil {
		return fleetCamp{}, err
	}
	targets := append([]machines.Machine(nil), mf.Machines...)
	if origin, ok := sessionHopOrigin(); ok && insideSSHSession() {
		m, registered := originTarget(origin)
		if !registered {
			targets = append(targets, *m)
		}
	}
	filter := listFilterFromScope(scope)
	results := fanOutRemote(ctx, targets, fleetEnumerator(filter))
	if err := ctx.Err(); err != nil {
		return fleetCamp{}, err
	}
	var live []fleetCamp
	var failed []string
	for _, result := range results {
		if result.err != nil {
			failed = append(failed, result.machineID+": "+formatUnreachableErr(result.err))
			continue
		}
		// Re-filter locally too: older remote binaries may ignore filters.
		for _, row := range filterEntries(result.rows, filter) {
			live = append(live, fleetCamp{
				Machine: result.machineID, Name: row.Name, ID: row.ID,
				Org: row.Org, Status: row.Status,
			})
		}
	}
	// Unreachable machines may contain another match. Neither uniqueness nor
	// absence can be established from a partial scan, regardless of the rows
	// that the successful machines returned.
	if len(failed) > 0 {
		verdict := "fleet lookup incomplete"
		if len(failed) == len(results) {
			verdict = "every remote machine failed to answer"
		}
		return fleetCamp{}, camperrors.Newf("camp %q: %s; failed machines: %s\nHint: run 'camp machine diagnose' or select a known target with 'csw machine:camp'",
			query, verdict, strings.Join(failed, "; "))
	}
	return matchFleetCamps(query, live)
}

func scopedFleetName(org, name string) string {
	if org != "" {
		return org + "/" + name
	}
	return name
}

// fleetSwitchSelector turns a fleet hit back into the machine:camp form the
// remote path already understands. hasTab keeps a `@tab` the user typed.
func fleetSwitchSelector(hit fleetCamp, hasTab bool, tab string) (cmdutil.ParsedMachineSelector, error) {
	name := hit.ID
	if name == "" {
		name = hit.Name
	}
	rem := scopedFleetName(hit.Org, name)
	if hasTab {
		rem += "@" + tab
	}
	return cmdutil.ParseMachineSelector(hit.Machine + ":" + rem)
}

func reportFleetMatch(w io.Writer, query string, hit fleetCamp) {
	if w == nil || query == hit.Name {
		return
	}
	_, _ = fmt.Fprintf(w, "Matched: %s -> %s:%s\n", query, hit.Machine, hit.Name)
}

// dispatchRemoteSwitch hops, or unwinds when this shell is already a hop.
// Unwind is what keeps a switch from nesting ssh sessions.
func dispatchRemoteSwitch(ctx context.Context, cmd *cobra.Command, msel cmdutil.ParsedMachineSelector, printOnly, shellConnect, jsonOut bool) error {
	if resume, unwind := unwindInsteadOfHop(msel); unwind {
		scope, err := switchScopeFromFlags(cmd)
		if err != nil {
			return err
		}
		// An explicit scope must be checked on the parent even for its own
		// camp. A plain exit cannot validate org or lifecycle membership.
		if resume == "" && (scope.Org != "" || scope.Status != "" || scope.All || cmdutil.ParseSwitchSelector(msel.Remainder).Org != "") {
			resume = "local:" + msel.Remainder
		}
		return emitHopUnwind(cmd, resume, printOnly, shellConnect, jsonOut)
	}
	return runRemoteSwitch(ctx, cmd, msel, printOnly, shellConnect, jsonOut)
}

// dispatchFleetSwitch resolves raw as a camp on another machine. handled is
// false when the fleet has nothing to say, so the caller keeps its local error.
func dispatchFleetSwitch(ctx context.Context, cmd *cobra.Command, raw string, scope cmdutil.CampaignScope, printOnly, shellConnect, jsonOut bool) (bool, error) {
	parsed, err := parseSwitchArg(raw, scope)
	if err != nil {
		return true, err
	}
	if parsed.Org != "" {
		scope.Org = parsed.Org
	}
	if strings.TrimSpace(parsed.Campaign) == "" {
		return false, nil
	}
	hit, err := lookupFleetCampaign(ctx, parsed.Campaign, scope)
	if errors.Is(err, errFleetMiss) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if hit.Machine == machines.LocalMachineID || isSelfMachine(ctx, hit.Machine) {
		return false, nil
	}
	reportFleetMatch(cmd.ErrOrStderr(), parsed.Campaign, hit)
	msel, err := fleetSwitchSelector(hit, parsed.HasTab, parsed.Tab)
	if err != nil {
		return true, err
	}
	return true, dispatchRemoteSwitch(ctx, cmd, msel, printOnly, shellConnect, jsonOut)
}
