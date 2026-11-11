package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	navfuzzy "github.com/Obedience-Corp/camp/internal/nav/fuzzy"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// errFleetMiss means no other machine has a camp for this query. Callers keep
// the local "not found" error in that case.
var errFleetMiss = errors.New("no matching camp on another machine")

// Fleet discovery is interactive: an unrelated offline peer must not consume
// the full SSH timeout before a healthy match can be selected. Explicit
// machine:camp switches keep the normal remote timeout.
const fleetLookupTimeout = 3 * time.Second

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

func lookupFleetCampaign(ctx context.Context, query string, scope cmdutil.CampaignScope) (fleetCamp, []remoteResult, error) {
	if err := ctx.Err(); err != nil {
		return fleetCamp{}, nil, err
	}
	mf, err := machines.Load()
	if err != nil {
		return fleetCamp{}, nil, err
	}
	targets := append([]machines.Machine(nil), mf.Machines...)
	if origin, ok := sessionHopOrigin(); ok && insideSSHSession() {
		m, registered := originTarget(origin)
		if !registered {
			targets = append(targets, *m)
		}
	}
	filter := listFilterFromScope(scope)
	lookupCtx, cancel := context.WithTimeout(ctx, fleetLookupTimeout)
	defer cancel()
	results := fanOutRemote(lookupCtx, targets, fleetEnumerator(filter))
	// A discovery deadline skips slow peers; cancellation of the command itself
	// still aborts the switch, even if another peer already returned a match.
	if err := ctx.Err(); err != nil {
		return fleetCamp{}, nil, err
	}
	var live []fleetCamp
	var failed []remoteResult
	for _, result := range results {
		if result.err != nil {
			failed = append(failed, result)
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
	// Resolve among machines that answered. An unavailable peer is not evidence
	// of ambiguity and must not veto a healthy match. Keep failures for a warning
	// on success, or diagnostics when none of the reachable camps matched.
	hit, err := matchFleetCamps(query, live)
	if !errors.Is(err, errFleetMiss) {
		return hit, failed, err
	}
	if len(failed) > 0 {
		verdict := "fleet lookup incomplete"
		if len(failed) == len(results) {
			verdict = "every remote machine failed to answer"
		}
		return fleetCamp{}, failed, fleetLookupFailure(query, verdict, failed)
	}
	return fleetCamp{}, nil, errFleetMiss
}

// fleetLookupFailure gives each failed machine its own line. A Tailscale
// approval URL gets a line to itself so it survives terminal wrapping and can
// be opened or copied whole.
func fleetLookupFailure(query, verdict string, failed []remoteResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "camp %q: %s; failed machines:", query, verdict)
	originID, originFailed := "", false
	if origin, ok := sessionHopOrigin(); ok && insideSSHSession() {
		m, _ := originTarget(origin)
		originID = m.ID
	}
	for _, result := range failed {
		b.WriteString("\n  " + fleetFailureLine(result))
		if originID != "" && result.machineID == originID {
			originFailed = true
		}
	}
	if originFailed {
		fmt.Fprintf(&b, "\nHint: %s opened this session. 'csw -' returns to it and 'csw %s:<camp>' switches to a camp on it, both without a new connection",
			originID, originID)
	}
	b.WriteString("\nHint: run 'camp machine diagnose' or select a known target with 'csw machine:camp'")
	return camperrors.New(b.String())
}

func fleetFailureLine(result remoteResult) string {
	id := result.machineID
	if url := remote.TailscaleCheckURL(result.err); url != "" {
		return id + ": Tailscale SSH needs a browser approval, then retry:\n      " + url
	}
	if detail := remote.HopFailureDetail(result.err); detail != "" {
		return id + ": " + detail
	}
	if remote.IsCampNotFound(result.err) {
		return id + ": camp is not installed there, or not on its login-shell PATH"
	}
	if errors.Is(result.err, context.DeadlineExceeded) {
		return id + ": timed out"
	}
	var cmdErr *camperrors.CommandError
	if errors.As(result.err, &cmdErr) && cmdErr.ExitCode != 0 {
		return id + ": " + firstSSHFailureLine(cmdErr)
	}
	return id + ": " + formatUnreachableErr(result.err)
}

// firstSSHFailureLine skips OpenSSH's "** WARNING" banner lines, which precede
// the real failure and are not the reason the command failed.
func firstSSHFailureLine(cmdErr *camperrors.CommandError) string {
	for _, line := range strings.Split(cmdErr.Stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "**") {
			continue
		}
		return line
	}
	return fmt.Sprintf("ssh exited with code %d", cmdErr.ExitCode)
}

// namesHopOriginCamp reports whether a bare query names the camp this hop was
// opened from. The shell underneath is still in that camp, so returning needs
// no lookup and no connection. A scope, org, or tab is a real selection the
// fleet path must resolve, so those do not qualify. The origin wins over a
// same-named camp on another machine.
func namesHopOriginCamp(parsed cmdutil.ParsedSwitchSelector, scope cmdutil.CampaignScope) bool {
	if parsed.Org != "" || parsed.HasTab || scope.Org != "" || scope.Status != "" || scope.All {
		return false
	}
	if !insideSSHSession() {
		return false
	}
	origin, ok := sessionHopOrigin()
	return ok && sameCampaignName(origin.Campaign, parsed.Campaign)
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
	if namesHopOriginCamp(parsed, scope) {
		return true, emitHopUnwind(cmd, "", printOnly, shellConnect, jsonOut)
	}
	if parsed.Org != "" {
		scope.Org = parsed.Org
	}
	if strings.TrimSpace(parsed.Campaign) == "" {
		return false, nil
	}
	hit, failed, err := lookupFleetCampaign(ctx, parsed.Campaign, scope)
	if errors.Is(err, errFleetMiss) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if hit.Machine == machines.LocalMachineID || isSelfMachine(ctx, hit.Machine) {
		return false, nil
	}
	if len(failed) > 0 {
		ids := make([]string, 0, len(failed))
		for _, result := range failed {
			ids = append(ids, result.machineID)
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "camp: skipped unavailable machines: %s (run 'camp machine diagnose' for details)\n", strings.Join(ids, ", "))
	}
	reportFleetMatch(cmd.ErrOrStderr(), parsed.Campaign, hit)
	msel, err := fleetSwitchSelector(hit, parsed.HasTab, parsed.Tab)
	if err != nil {
		return true, err
	}
	return true, dispatchRemoteSwitch(ctx, cmd, msel, printOnly, shellConnect, jsonOut)
}
