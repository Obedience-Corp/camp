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
		key := c.Machine + "\n" + c.Name
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
		label := h.Machine + ":" + h.Name
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

// fleetCandidatesFromCache is the no-network view: the hop origin's camp, plus
// every machine's cached names (including a stale pull cache).
func fleetCandidatesFromCache() []fleetCamp {
	var out []fleetCamp
	if c, ok := originFleetCandidate(); ok {
		out = append(out, c)
	}
	mf, err := machines.Load()
	if err != nil {
		return dedupeFleetCamps(out)
	}
	for _, m := range mf.Machines {
		names, ok := readMachineCacheCampaignsIncludingStale(m.ID)
		if !ok {
			continue
		}
		for _, name := range names {
			out = append(out, fleetCamp{Machine: m.ID, Name: name})
		}
	}
	return dedupeFleetCamps(out)
}

func originFleetCandidate() (fleetCamp, bool) {
	origin, ok := sessionHopOrigin()
	if !ok || origin.Campaign == "" {
		return fleetCamp{}, false
	}
	id := origin.ID
	if mf, err := machines.Load(); err == nil {
		if id != "" {
			if m, _, found := mf.Lookup(id); found && m != nil {
				id = m.ID
			}
		}
		if id == "" || !fleetIDKnown(mf, id) {
			want := strings.ToLower(normalizeDNSName(origin.Host))
			for i := range mf.Machines {
				if strings.ToLower(normalizeDNSName(mf.Machines[i].Host)) == want {
					id = mf.Machines[i].ID
					break
				}
			}
		}
	}
	if id == "" {
		id = suggestedMachineID(origin.Host)
	}
	if id == "" {
		return fleetCamp{}, false
	}
	return fleetCamp{Machine: id, Name: origin.Campaign}, true
}

func fleetIDKnown(mf *machines.File, id string) bool {
	if mf == nil || id == "" {
		return false
	}
	_, _, found := mf.Lookup(id)
	return found
}

func scopeRestrictsFleet(scope cmdutil.CampaignScope) bool {
	return scope.Org != "" || scope.Status != "" || scope.All
}

// lookupFleetCampaign resolves a bare camp name that missed the local registry.
// Cache and the hop origin are tried first so a warm fleet does not ssh. A miss
// there enumerates ~/.obey/machines.yaml, because a name that was never cached
// is still a real camp.
func lookupFleetCampaign(ctx context.Context, query string, scope cmdutil.CampaignScope) (fleetCamp, error) {
	if !scopeRestrictsFleet(scope) {
		hit, err := matchFleetCamps(query, fleetCandidatesFromCache())
		if err == nil || !errors.Is(err, errFleetMiss) {
			return hit, err
		}
	}

	mf, loadErr := machines.Load()
	originHit, hasOrigin := originFleetCandidate()
	if loadErr != nil {
		if hasOrigin {
			return matchFleetCamps(query, []fleetCamp{originHit})
		}
		return fleetCamp{}, errFleetMiss
	}
	if len(mf.Machines) == 0 {
		if hasOrigin {
			return matchFleetCamps(query, []fleetCamp{originHit})
		}
		return fleetCamp{}, errFleetMiss
	}

	rows, results, err := loadRemoteCampaigns(ctx, listFilterFromScope(scope))
	if err != nil {
		return fleetCamp{}, err
	}
	live := make([]fleetCamp, 0, len(rows)+1)
	if hasOrigin {
		live = append(live, originHit)
	}
	for _, row := range rows {
		if row.Machine == "" || row.Machine == machines.LocalMachineID {
			continue
		}
		live = append(live, fleetCamp{Machine: row.Machine, Name: row.Name})
	}
	hit, matchErr := matchFleetCamps(query, live)
	if matchErr == nil || !errors.Is(matchErr, errFleetMiss) {
		return hit, matchErr
	}
	if len(rows) == 0 && remoteAttemptsAllFailed(results) {
		return fleetCamp{}, camperrors.New(fmt.Sprintf("camp %q was not found on this machine, and every remote machine failed to answer\nHint: run 'camp machine diagnose'", query))
	}
	return fleetCamp{}, errFleetMiss
}

func remoteAttemptsAllFailed(results []remoteResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, r := range results {
		if r.err == nil {
			return false
		}
	}
	return true
}

// fleetSwitchSelector turns a fleet hit back into the machine:camp form the
// remote path already understands. hasTab keeps a `@tab` the user typed.
func fleetSwitchSelector(hit fleetCamp, hasTab bool, tab string) (cmdutil.ParsedMachineSelector, error) {
	rem := hit.Name
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
		return emitHopUnwind(cmd, resume, printOnly, shellConnect, jsonOut)
	}
	return runRemoteSwitch(ctx, cmd, msel, printOnly, shellConnect, jsonOut)
}

// dispatchFleetSwitch resolves raw as a camp on another machine. handled is
// false when the fleet has nothing to say, so the caller keeps its local error.
func dispatchFleetSwitch(ctx context.Context, cmd *cobra.Command, raw string, scope cmdutil.CampaignScope, printOnly, shellConnect, jsonOut bool) (bool, error) {
	parsed, err := parseSwitchArg(raw, scope)
	if err != nil {
		return false, err
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
		return false, err
	}
	if hit.Machine == machines.LocalMachineID || isSelfMachine(ctx, hit.Machine) {
		return false, nil
	}
	reportFleetMatch(cmd.ErrOrStderr(), parsed.Campaign, hit)
	msel, err := fleetSwitchSelector(hit, parsed.HasTab, parsed.Tab)
	if err != nil {
		return false, err
	}
	return true, dispatchRemoteSwitch(ctx, cmd, msel, printOnly, shellConnect, jsonOut)
}
