//go:build dev

// Package audit implements the `camp audit` command group: the bypass-tolerance
// doctor (D004). `camp audit doctor` scans linked repos for commits with no
// captured intent linkage and reports them informationally (untagged commits
// are a normal mode, never a violation; exit 0 on findings).
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/audit"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// Cmd is the `camp audit` command group.
var Cmd = &cobra.Command{
	Use:   "audit",
	Short: "Inspect the campaign audit trail",
	Long: `Inspect the campaign audit trail.

'camp audit doctor' scans linked repos for commits with no captured intent
linkage and reports them informationally. Untagged commits are a normal mode
for wrapper-opt-out workflows, not a violation.`,
}

func init() {
	Cmd.AddCommand(newDoctorCommand())
}

func newDoctorCommand() *cobra.Command {
	var (
		window  int
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Scan linked repos for unattributed commits (informational)",
		Long: `Scan the campaign root and every linked project repo, classifying each
commit as tagged, degraded, or untagged (no captured intent linkage).

Output is informational: untagged commits are surfaced, never scolded, and the
command exits 0 even when findings exist. Use --window to bound each repo to its
most recent N commits (default: full history).`,
		Args: cobra.NoArgs,
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only scan with --json output for automation",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd, window, jsonOut)
		},
	}
	cmd.Flags().IntVar(&window, "window", 0, "scan only the most recent N commits per repo (0 = full history)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON report")
	return cmd
}

func runDoctor(cmd *cobra.Command, window int, jsonOut bool) error {
	ctx := cmd.Context()
	cfg, campRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	repos, err := scanTargets(ctx, campRoot)
	if err != nil {
		return err
	}

	scans := make([]audit.RepoScan, 0, len(repos))
	var totalUntagged, totalCommits int
	for _, r := range repos {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		scan, scanErr := audit.ScanRepo(ctx, r.label, r.path, window)
		if scanErr != nil {
			return scanErr
		}
		scans = append(scans, scan)
		totalUntagged += scan.Untagged
		totalCommits += scan.Total
	}

	// Second pass (D004): reconciliation gaps, read-only. The doctor surfaces
	// how many state-file facts the ledger does not capture; the actual write is
	// opt-in via `camp audit reconcile --apply`.
	gaps, err := audit.Reconcile(ctx, campRoot, cfg.ID)
	if err != nil {
		return err
	}

	if jsonOut {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			SchemaVersion string           `json:"schema_version"`
			TotalCommits  int              `json:"total_commits"`
			TotalUntagged int              `json:"total_untagged"`
			ReconcileGaps int              `json:"reconcile_gaps"`
			Repos         []audit.RepoScan `json:"repos"`
		}{"camp-audit-doctor/v1", totalCommits, totalUntagged, len(gaps), scans})
	}

	return printReport(cmd, scans, totalCommits, totalUntagged, len(gaps))
}

func printReport(cmd *cobra.Command, scans []audit.RepoScan, totalCommits, totalUntagged, reconcileGaps int) error {
	w := cmd.OutOrStdout()
	if _, err := fmt.Fprintln(w, ui.Subheader("Campaign audit trail: commit attribution")); err != nil {
		return err
	}
	for _, s := range scans {
		pct := 0.0
		if s.Total > 0 {
			pct = 100 * float64(s.Untagged) / float64(s.Total)
		}
		if _, err := fmt.Fprintf(w, "  %-16s tagged %-5d degraded %-3d untagged %-5d / %-5d (%.0f%% unattributed)\n",
			s.Repo, s.Tagged, s.Degraded, s.Untagged, s.Total, pct); err != nil {
			return err
		}
	}
	overall := 0.0
	if totalCommits > 0 {
		overall = 100 * float64(totalUntagged) / float64(totalCommits)
	}
	if _, err := fmt.Fprintf(w, "\n%s %d of %d commits (%.0f%%) have no captured intent linkage.\n",
		ui.InfoIcon(), totalUntagged, totalCommits, overall); err != nil {
		return err
	}
	if reconcileGaps > 0 {
		if _, err := fmt.Fprintf(w, "%s %d state-file fact(s) are not in the ledger. Run 'camp audit reconcile --apply' to record them.\n",
			ui.InfoIcon(), reconcileGaps); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w, ui.Dim("  This is informational. Capture in state-changing commands (not commit discipline) is the trail; reconciliation and opt-in repair attribute the rest."))
	return err
}

type scanTarget struct {
	label string
	path  string
}

// scanTargets returns the campaign root plus every linked submodule repo,
// each with its campaign-relative label.
func scanTargets(ctx context.Context, campRoot string) ([]scanTarget, error) {
	targets := []scanTarget{{label: "campaign-root", path: campRoot}}
	subs, err := git.ListSubmodulePaths(ctx, campRoot)
	if err != nil {
		// No .gitmodules or no submodules is fine: scan the root only.
		return targets, nil
	}
	for _, rel := range subs {
		targets = append(targets, scanTarget{
			label: ledger.RepoLabel(campRoot, filepath.Join(campRoot, rel)),
			path:  filepath.Join(campRoot, rel),
		})
	}
	return targets, nil
}
