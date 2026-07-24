package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// remoteResult is one machine's contribution to `camp list --remote`. A non-nil
// err means the machine was unreachable/failed and becomes a labeled row (task 2's
// renderer); it never contaminates or delays the other machines' rows.
type remoteResult struct {
	machineID string
	rows      []campaignEntry
	err       error
}

// enumerateFunc resolves one machine's campaigns. Real code uses enumerateRemote
// (ssh-exec); tests inject a fake so the fan-out is exercised without ssh.
type enumerateFunc func(context.Context, *machines.Machine) ([]campaignEntry, error)

// fanOutRemote runs enumerate against every machine concurrently and collects the
// results into a fixed-index slice (no shared-append race). Each enumeration is
// bounded by remote.Run's own timeout, so one dead machine never blocks another
// and the whole command returns promptly.
func fanOutRemote(ctx context.Context, ms []machines.Machine, enumerate enumerateFunc) []remoteResult {
	results := make([]remoteResult, len(ms))
	var wg sync.WaitGroup
	for i := range ms {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rows, err := enumerate(ctx, &ms[i])
			results[i] = remoteResult{machineID: ms[i].ID, rows: rows, err: err}
		}(i)
	}
	wg.Wait()
	return results
}

// remoteListArgs re-expresses the local list filter as flags on the remote
// `list --json` (the args RunCampCommand appends after the resolved camp
// binary). Passing --all/--status is a correctness requirement, not just
// payload reduction: the remote default emits active campaigns only, so any
// row outside the active set must be explicitly requested or it is never fetched
// (a local re-filter cannot recover rows the remote never sent). --org/--tag also
// shrink the ssh payload. renderListTable still re-filters the combined set as the
// authoritative backstop against a version-skewed or looser remote.
func remoteListArgs(f listFilter) string {
	cmd := "list --json"
	if f.org != "" {
		cmd += " --org " + remote.ShellQuote(f.org)
	}
	for _, t := range f.tags {
		cmd += " --tag " + remote.ShellQuote(t)
	}
	if f.status != "" {
		cmd += " --status " + remote.ShellQuote(f.status)
	}
	if f.all {
		cmd += " --all"
	}
	return cmd
}

// enumerateRemoteFor returns an enumerateFunc that runs the remote machine's OWN
// `camp list --json` (with the active filter re-expressed as flags) over ssh, so
// its registry, org config, and absolute paths are authoritative, then re-tags
// each row with the machine id. Remote paths are left verbatim (meaningful only on
// the far machine).
func enumerateRemoteFor(f listFilter) enumerateFunc {
	args := remoteListArgs(f)
	return func(ctx context.Context, m *machines.Machine) ([]campaignEntry, error) {
		out, err := remote.RunCampCommand(ctx, m, args)
		if err != nil {
			return nil, err
		}
		var rows []campaignEntry
		if err := json.Unmarshal(out, &rows); err != nil {
			return nil, camperrors.Wrap(err, "parse remote camp list --json")
		}
		names := make([]string, 0, len(rows))
		for i := range rows {
			rows[i].Machine = m.ID
			// Preserve the []-not-null tags guard on re-emitted rows so a Rust Vec<T>
			// (or any strict) consumer of `camp list --json` never sees null tags.
			if rows[i].Tags == nil {
				rows[i].Tags = []string{}
			}
			names = append(names, rows[i].Name)
		}
		// Warm the completion cache so `csw <id>:<tab>` has campaigns without the
		// keystroke path ever doing a live ssh.
		writeMachineCacheCampaigns(m.ID, names)
		return rows, nil
	}
}

// loadRemoteCampaigns is the shared live fan-out used by list --remote, the list
// TUI remote toggle, and the switch picker. It loads machines.yaml, enumerates
// every machine, and returns successful rows plus the full result set (including
// per-machine errors). A missing/empty fleet returns empty slices, not an error.
// Callers re-filter as needed; this does not re-apply the local filter backstop.
func loadRemoteCampaigns(ctx context.Context, filter listFilter) ([]campaignEntry, []remoteResult, error) {
	mf, err := machines.Load()
	if err != nil {
		return nil, nil, err
	}
	if len(mf.Machines) == 0 {
		return nil, nil, nil
	}
	results := fanOutRemote(ctx, mf.Machines, enumerateRemoteFor(filter))
	var rows []campaignEntry
	for _, r := range results {
		if r.err == nil {
			rows = append(rows, r.rows...)
		}
	}
	return rows, results, nil
}

// listFilterFromScope maps a switch CampaignScope onto the list filter used for
// remote enumerate (active-only by default; --all / --status / --org forwarded).
func listFilterFromScope(scope cmdutil.CampaignScope) listFilter {
	return listFilter{
		org:    scope.Org,
		status: scope.Status,
		all:    scope.All,
	}
}

// hasRemoteDimension reports whether the output involves any machine other than
// the local one — a reachable remote row or an unreachable machine. Only then does
// the human table gain a MACHINE column, so a single-machine user sees no change.
func hasRemoteDimension(campaigns []campaignEntry, results []remoteResult) bool {
	for _, c := range campaigns {
		if c.Machine != "" && c.Machine != machines.LocalMachineID {
			return true
		}
	}
	for _, r := range results {
		if r.err != nil {
			return true
		}
	}
	return false
}

// outputRemoteList renders `camp list --remote`. JSON stays the successful-row
// contract (unreachable machines go to stderr so stdout stays parseable); the
// table gains a leading MACHINE column only when a remote machine is present and
// appends one muted labeled row per unreachable machine. With only local rows it
// defers to outputCampaigns, so single-machine output is identical to today.
func outputRemoteList(stdout, stderr io.Writer, campaigns []campaignEntry, results []remoteResult, format string) error {
	switch format {
	case "json":
		warnUnreachable(stderr, results)
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(campaigns)
	case "simple":
		for _, c := range campaigns {
			if _, err := fmt.Fprintln(stdout, c.Name); err != nil {
				return err
			}
		}
		return nil
	default: // table
		if !hasRemoteDimension(campaigns, results) {
			return outputCampaigns(stdout, campaigns, format)
		}
		return renderRemoteTable(stdout, campaigns, results)
	}
}

func warnUnreachable(stderr io.Writer, results []remoteResult) {
	for _, r := range results {
		if r.err != nil {
			_, _ = fmt.Fprintln(stderr, ui.Warning(fmt.Sprintf("machine %s unreachable: %s", r.machineID, formatUnreachableErr(r.err))))
		}
	}
}

// formatUnreachableErr prefers classified hop failures (check-mode, host-key,
// publickey) over multi-line wrapped ssh noise so list --remote stays readable.
func formatUnreachableErr(err error) string {
	if err == nil {
		return ""
	}
	if detail := remote.HopFailureDetail(err); detail != "" {
		return detail
	}
	msg := strings.TrimSpace(err.Error())
	if line, _, ok := strings.Cut(msg, "\n"); ok {
		msg = strings.TrimSpace(line)
	}
	return msg
}

func renderRemoteTable(stdout io.Writer, campaigns []campaignEntry, results []remoteResult) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	var werr error
	p := func(format string, a ...any) {
		if werr == nil {
			_, werr = fmt.Fprintf(w, format, a...)
		}
	}
	p("%s\t%s\t%s\t%s\t%s\t%s\n",
		ui.Label("MACHINE"), ui.Label("ID"), ui.Label("NAME"), ui.Label("ORG"), ui.Label("TYPE"), ui.Label("PATH"))
	for _, c := range campaigns {
		id, name, org, typ, path := campaignTableCells(c)
		machine := c.Machine
		if machine == "" {
			machine = machines.LocalMachineID
		}
		p("%s\t%s\t%s\t%s\t%s\t%s\n", ui.Label(machine), id, name, org, typ, path)
	}
	for _, r := range results {
		if r.err != nil {
			p("%s\t%s\t\t\t\t\n", ui.Dim(r.machineID), ui.Dim(fmt.Sprintf("(unreachable: %s)", formatUnreachableErr(r.err))))
		}
	}
	if werr != nil {
		return werr
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return err
	}
	_, err := fmt.Fprintln(stdout, ui.Dim(ui.CountLabel(len(campaigns), "campaign", "campaigns")))
	return err
}
