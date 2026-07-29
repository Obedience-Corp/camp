package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	navfuzzy "github.com/Obedience-Corp/camp/internal/nav/fuzzy"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// resolveRemoteRoot is the seam every remote switch resolves through, hop-back
// and machine:campaign alike, so both can be tested without a live ssh.
// Production is remote.ResolveRoot; tests substitute a stub.
//
// Not parallel-safe: tests swap this package-level var, so a test that does so
// must not call t.Parallel().
var resolveRemoteRoot = remote.ResolveRoot

// runRemoteSwitch resolves a machine:campaign selector by asking the remote
// machine's own `camp switch --print` for the absolute campaign root over ssh (so
// the remote registry decides the path), then emits the interactive ssh hop line
// under --shell-connect. --print is local-only: it MUST print nothing for a remote
// target so `cd "$(camp switch x --print)"` fails loudly instead of cd-ing wrong.
func runRemoteSwitch(ctx context.Context, cmd *cobra.Command, msel cmdutil.ParsedMachineSelector, printOnly, shellConnect, jsonOut bool) error {
	if err := guardRemoteOutputFlags(msel.Machine, printOnly, jsonOut); err != nil {
		return err
	}
	mf, err := machines.Load()
	if err != nil {
		return err
	}
	m, _, found := mf.Lookup(msel.Machine)
	if !found {
		return camperrors.New("unknown machine \"" + msel.Machine + "\"; add it to ~/.obey/machines.yaml")
	}
	root, err := resolveRemoteRoot(ctx, m, msel.Remainder)
	if err != nil {
		return withRemoteSuggestions(err, msel)
	}
	return emitHopOrRefuse(ctx, cmd, m, msel.Remainder, root, shellConnect)
}

// guardRemoteOutputFlags rejects the two flags that cannot mean anything for a
// hop. Shared by every remote entry point so `camp switch -` and
// `camp switch machine:campaign` refuse identically rather than drifting.
func guardRemoteOutputFlags(target string, printOnly, jsonOut bool) error {
	if printOnly {
		return camperrors.New("remote target " + target +
			": --print is local-only; use the csw shell wrapper to hop there")
	}
	if jsonOut {
		return camperrors.New("--json is not supported for a remote (machine:) switch; use the csw shell wrapper")
	}
	return nil
}

// emitHopOrRefuse is the shared tail of every hop: emit the eval line under
// --shell-connect, or resolve-and-refuse otherwise. The refusal is the agent
// safety contract — a bare `camp switch machine:...` must never hop, so it
// reports what it resolved and names the wrapper instead.
func emitHopOrRefuse(ctx context.Context, cmd *cobra.Command, m *machines.Machine, remainder, root string, shellConnect bool) error {
	// Skew is reported on stderr, never stdout: stdout is eval'd by the shell
	// wrapper and must stay exactly one line.
	if warning := hopSkewWarning(m.ID); warning != "" {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), ui.Dim("camp: "+warning))
	}
	if shellConnect {
		return emitShellConnect(cmd.OutOrStdout(), true, root, m, hopOriginForEmit(ctx, cmd))
	}
	return camperrors.New("resolved " + m.ID + ":" + remainder + " -> " + root +
		"; run via the csw shell wrapper to hop there")
}

// hopBackHintSuffix appends the adopt suggestion to a hop-back failure when the
// origin is unregistered here. Registering is exactly what would have made the
// failure diagnosable with `camp machine diagnose`, so the suggestion is on
// topic rather than generic advice bolted onto an error.
func hopBackHintSuffix() string {
	if hint := OriginHint(); hint != "" {
		return "; 'camp machine adopt' registers it so future hops know the way back"
	}
	return ""
}

// hopOriginForEmit builds the CAMP_HOP_ORIGIN payload describing THIS machine,
// for the session on the far side. Every failure yields "" (no export), never
// an error: the operator asked to hop, not to advertise an origin, so a machine
// with no reachable name still hops exactly as it does today. A warning goes to
// stderr because stdout is eval'd by the shell wrapper and must stay one line.
//
// This runs on the reverse hop too, which is what makes `camp switch -` a
// toggle rather than a one-way trip: the session it lands in knows the machine
// it just left.
func hopOriginForEmit(ctx context.Context, cmd *cobra.Command) string {
	originCampaign := ""
	if root, err := campaign.DetectCached(ctx); err == nil {
		if cfg, err := config.LoadCampaignConfig(ctx, root); err == nil {
			originCampaign = cfg.Name
		}
	}
	origin, err := buildHopOrigin(ctx, originCampaign, runTailscaleStatusForSelf)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"camp: warning: hop origin unavailable (%v); 'camp switch -' will not work from that session\n", err)
		return ""
	}
	return origin
}

// withRemoteSuggestions augments a remote resolve failure with near matches
// from the per-machine completion cache. Cache-only on purpose: the failed
// resolve already paid one SSH round-trip, and suggestions must not add
// another. A cold cache simply yields no suggestions.
func withRemoteSuggestions(err error, msel cmdutil.ParsedMachineSelector) error {
	names, ok := readMachineCacheCampaigns(msel.Machine)
	if !ok || len(names) == 0 {
		return err
	}
	query := cmdutil.ParseSwitchSelector(msel.Remainder).Campaign
	if query == "" {
		return err
	}
	matches := navfuzzy.Filter(names, query)
	if len(matches) == 0 {
		return err
	}
	limit := min(len(matches), 3)
	suggestions := make([]string, 0, limit)
	for _, match := range matches[:limit] {
		suggestions = append(suggestions, msel.Machine+":"+match.Target)
	}
	return camperrors.Wrapf(err, "did you mean %s?", strings.Join(suggestions, ", "))
}
