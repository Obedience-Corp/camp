package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/approvals"
	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/jobs"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/pkg/approvalkit"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// ApprovalsJSONVersion is the schema of `camp approvals --json`.
const ApprovalsJSONVersion = "approvals/v1alpha1"

var approvalsCmd = &cobra.Command{
	Use:   "approvals",
	Short: "See what in this camp is waiting on your approval",
	Long: `List everything in this camp that is waiting on a human to approve.

Agents stop for a human in several places: a festival workflow checkpoint, a
phase gate, a checkpoint in a standalone WORKFLOW.md. Each one records a request
in the camp's approvals ledger when it starts waiting and a resolution when it
stops, so this is the one place to look.

Run bare to browse the list, read what each request asks, and approve or reject
it. Approving hands your terminal to the source's own command, run from the
source's directory and pinned to the step you were shown; camp never records an
approval itself. Agents can read this list with --json. They cannot approve
through it: the source's command requires a person at a terminal.

The ledger lives in .campaign/approvals and is committed, so a request made on
one machine shows up on your others after a push and pull. Each event is
committed when it is recorded; listing also commits any that were missed (pass
--no-commit to skip that; --json never commits). A request
whose source is not on this machine, or has moved on, is still listed and
marked as not actionable here.

Examples:
  camp approvals                   # interactive browser in a TTY; table otherwise
  camp approvals --plain           # always print the table
  camp approvals --json            # the same, for scripts and agents
  camp approvals show <id>         # one request in full
  camp approvals path <id>         # the source directory, to cd into
  camp approvals withdraw <id> --reason "..."   # clear a request whose source is gone
  camp approvals withdraw --stale               # clear every request whose source moved past it`,
	Args: cobra.NoArgs,
	RunE: jsoncontract.RunE(ApprovalsJSONVersion, func() bool { return approvalsOpts.json }, runApprovalsList),
}

var approvalsShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show one approval request in full",
	Long: `Print everything a request says: what is asked, which step is waiting, where
its source lives, who asked, and whether it can be acted on from this machine.

<id> is the short id from 'camp approvals', or an unambiguous prefix of it.`,
	Args: cobra.ExactArgs(1),
	RunE: jsoncontract.RunE(ApprovalsJSONVersion, func() bool { return approvalsOpts.json }, runApprovalsShow),
}

var approvalsPathCmd = &cobra.Command{
	Use:   "path <id>",
	Short: "Print the directory of a request's source",
	Long: `Print the absolute directory a request's source lives in, and nothing else,
so a shell can use it:

  cd "$(camp approvals path <id>)"

It fails when the directory is not on this machine or is not inside this camp.`,
	Args: cobra.ExactArgs(1),
	RunE: runApprovalsPath,
}

var approvalsWithdrawCmd = &cobra.Command{
	Use:   "withdraw [id]",
	Short: "Clear a request whose source is no longer waiting",
	Long: `Record that a request is no longer waiting, without approving or rejecting
anything.

Use it for a request that outlived its source: the workflow was deleted, the
worktree is gone, or the request was recorded by mistake. Withdrawing only
removes the request from this list. It does not unblock anything, because the
ledger never grants an approval: if the source is in fact still waiting, it
reports itself again the next time it is advanced.

--stale withdraws, without naming them, every request that can no longer be
decided: its source is on this machine and has finished, or is already past the
recorded step. Everything else is left alone. A source that is behind the
recorded step usually means this checkout has not caught up after a pull, and a
request whose source cannot be read here may be waiting on another machine; a
withdrawal is recorded in the shared ledger, so it would clear the request
there too.

Withdrawing clears that one request. Another request from the same source
stays pending.

Examples:
  camp approvals withdraw ap-1b9b5140 --reason "workflow deleted"
  camp approvals withdraw --stale`,
	Args: cobra.MaximumNArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Clearing a pending approval request is a human decision",
	},
	RunE: runApprovalsWithdraw,
}

var approvalsCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit approval events that are not committed yet",
	Long: `Commit .campaign/approvals and nothing else.

Approval events are committed when they are recorded, and listing approvals
commits any that were missed. This does only the commit, for a script or before
a push, when an earlier commit could not be made.`,
	Args: cobra.NoArgs,
	RunE: runApprovalsCommit,
}

var approvalsOpts struct {
	json        bool
	plain       bool
	interactive bool
	noCommit    bool
	reason      string
	stale       bool
}

func init() {
	approvalsCmd.PersistentFlags().BoolVar(&approvalsOpts.json, "json", false,
		"Emit a structured JSON result")
	approvalsCmd.Flags().BoolVar(&approvalsOpts.plain, "plain", false,
		"Print the table even when stdout is a terminal")
	approvalsCmd.Flags().BoolVarP(&approvalsOpts.interactive, "interactive", "i", false,
		"Open the interactive approvals browser (prints the table when stdout is not a terminal)")
	approvalsCmd.Flags().BoolVar(&approvalsOpts.noCommit, "no-commit", false,
		"Do not commit uncommitted approval events")
	approvalsWithdrawCmd.Flags().StringVar(&approvalsOpts.reason, "reason", "",
		"Why the request is being withdrawn (required with an id)")
	approvalsWithdrawCmd.Flags().BoolVar(&approvalsOpts.stale, "stale", false,
		"Withdraw every request whose source reports it is no longer waiting")

	approvalsCmd.AddCommand(approvalsShowCmd, approvalsPathCmd, approvalsWithdrawCmd, approvalsCommitCmd)
	rootCmd.AddCommand(approvalsCmd)
	approvalsCmd.GroupID = "planning"
	approvalsCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(ApprovalsJSONVersion, func() bool { return approvalsOpts.json }))
}

// approvalRow is one pending request plus what this machine can do with it.
type approvalRow struct {
	approvalkit.Pending
	ready approvals.Readiness
}

// approvalsSnapshot is everything one look at the inbox shows.
type approvalsSnapshot struct {
	campRoot string
	rows     []approvalRow
	// behind is how many commits the camp is behind its upstream; behindKnown
	// is false when there is no upstream to compare with.
	behind      int
	behindKnown bool
	// uncommitted counts approvals ledger files with uncommitted changes.
	uncommitted int
}

func loadApprovals(ctx context.Context, campRoot string) (approvalsSnapshot, error) {
	snap := approvalsSnapshot{campRoot: campRoot}
	pending, err := approvalkit.Load(ctx, campRoot)
	if err != nil {
		return snap, camperrors.Wrap(err, "reading approvals")
	}
	ready := approvals.CheckAll(ctx, campRoot, pending)
	snap.rows = make([]approvalRow, 0, len(pending))
	for i, p := range pending {
		snap.rows = append(snap.rows, approvalRow{Pending: p, ready: ready[i]})
	}
	snap.behind, snap.behindKnown = git.BehindUpstream(ctx, campRoot)
	snap.uncommitted = approvalkit.Uncommitted(ctx, campRoot)
	return snap, nil
}

// commitApprovals commits uncommitted approval events unless the user opted
// out, and returns a one-line note about what it did ("" when nothing is worth
// saying). It never fails the command: the events are on disk either way.
func commitApprovals(ctx context.Context, campRoot string) string {
	if approvalsOpts.noCommit {
		return ""
	}
	res, err := approvalkit.Commit(ctx, campRoot)
	if err != nil {
		return "Warning: could not commit approval events: " + err.Error()
	}
	if res.Status != approvalkit.CommitCreated {
		return ""
	}
	hash := res.Hash
	if len(hash) > 7 {
		hash = hash[:7]
	}
	return "committed approval events (" + hash + ")"
}

func runApprovalsList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp")
	}
	// --json is what agents and apps poll. It only reads: a caller that asks
	// for a list should not find it made a commit, or ran the repository's
	// commit hooks. The uncommitted count in the document says when
	// 'camp approvals commit' is worth running.
	note := ""
	if !approvalsOpts.json {
		note = commitApprovals(ctx, campRoot)
	}
	snap, err := loadApprovals(ctx, campRoot)
	if err != nil {
		return err
	}
	if approvalsOpts.json {
		return emitApprovalsJSON(cmd, snap)
	}
	if note != "" {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), note)
	}
	if approvalsTUIRequested(stdoutIsTTY()) {
		return runApprovalsTUI(cmd, snap)
	}
	renderApprovalsHuman(cmd, snap)
	return nil
}

// approvalsTUIRequested decides whether bare `camp approvals` opens the
// browser. --json and --plain never do; -i forces; otherwise an interactive
// terminal opens it.
func approvalsTUIRequested(isTTY bool) bool {
	if approvalsOpts.json || approvalsOpts.plain {
		return false
	}
	if approvalsOpts.interactive {
		return true
	}
	return isTTY
}

// approvalsPayload is the --json document.
type approvalsPayload struct {
	SchemaVersion string `json:"schema_version"`
	CampaignRoot  string `json:"campaign_root"`
	Pending       int    `json:"pending"`
	// BehindUpstream is how many commits the camp is behind its upstream,
	// from refs already fetched. Absent when there is no upstream. When it is
	// greater than zero the list may be missing requests made elsewhere.
	BehindUpstream *int `json:"behind_upstream,omitempty"`
	// Uncommitted counts approvals ledger files with uncommitted changes:
	// events that have not reached the camp's history, so not other machines.
	Uncommitted int `json:"uncommitted"`
	// Approvals is always an array, never null.
	Approvals []approvalJSON `json:"approvals"`
}

// approvalJSON is one row: the request as recorded, plus what this machine
// can do with it.
type approvalJSON struct {
	approvalkit.Pending
	AgeMs int64 `json:"age_ms"`
	// Actionable is true when the source is on this machine and waiting at the
	// recorded step. NotActionableReason says why when it is not.
	Actionable          bool   `json:"actionable"`
	NotActionableReason string `json:"not_actionable_reason,omitempty"`
	// Dir is the absolute source directory. Present only when actionable.
	Dir string `json:"dir,omitempty"`
	// ApproveCommand is what a person runs in Dir to approve, chosen by camp
	// from the source kind. It requires an interactive terminal. Present only
	// when actionable.
	ApproveCommand []string `json:"approve_command,omitempty"`
}

func approvalToJSON(row approvalRow, now time.Time) approvalJSON {
	out := approvalJSON{
		Pending:             row.Pending,
		AgeMs:               approvalAge(row.Pending, now).Milliseconds(),
		Actionable:          row.ready.Actionable,
		NotActionableReason: row.ready.Reason,
	}
	if row.ready.Actionable {
		out.Dir = row.ready.Dir
		if argv, err := approvals.Command(row.Pending, approvals.Approve, ""); err == nil {
			out.ApproveCommand = argv
		}
	}
	return out
}

func approvalAge(p approvalkit.Pending, now time.Time) time.Duration {
	t, err := time.Parse(time.RFC3339Nano, p.RequestedAt)
	if err != nil {
		return 0
	}
	if d := now.Sub(t); d > 0 {
		return d
	}
	return 0
}

func emitApprovalsJSON(cmd *cobra.Command, snap approvalsSnapshot) error {
	now := time.Now()
	payload := approvalsPayload{
		SchemaVersion: ApprovalsJSONVersion,
		CampaignRoot:  snap.campRoot,
		Pending:       len(snap.rows),
		Uncommitted:   snap.uncommitted,
		Approvals:     make([]approvalJSON, 0, len(snap.rows)),
	}
	if snap.behindKnown {
		behind := snap.behind
		payload.BehindUpstream = &behind
	}
	for _, row := range snap.rows {
		payload.Approvals = append(payload.Approvals, approvalToJSON(row, now))
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// approvalsNotices are the lines that qualify the list: why it might be
// incomplete, and what has not been shared yet.
func approvalsNotices(snap approvalsSnapshot) []string {
	var notes []string
	if snap.behindKnown && snap.behind > 0 {
		notes = append(notes, fmt.Sprintf(
			"This camp is %s behind its upstream; requests made on other machines may be missing until you pull.",
			ui.CountLabel(snap.behind, "commit", "commits")))
	}
	if snap.uncommitted > 0 {
		notes = append(notes, fmt.Sprintf(
			"%s not committed; other machines will not see them yet.",
			ui.CountLabel(snap.uncommitted, "approvals file is", "approvals files are")))
	}
	return notes
}

func approvalSourceLabel(kind string) string {
	switch kind {
	case approvalkit.SourceFestivalCheckpoint:
		return "festival"
	case approvalkit.SourceStandaloneCheckpoint:
		return "workflow"
	case approvalkit.SourcePhaseGate:
		return "gate"
	case "":
		return "unknown"
	default:
		return kind
	}
}

func renderApprovalsHuman(cmd *cobra.Command, snap approvalsSnapshot) {
	out := cmd.OutOrStdout()
	for _, note := range approvalsNotices(snap) {
		_, _ = fmt.Fprintln(out, ui.Warning(note))
	}
	if len(snap.rows) == 0 {
		_, _ = fmt.Fprintln(out, ui.Dim("Nothing is waiting on your approval."))
		return
	}
	now := time.Now()
	var b strings.Builder
	fmt.Fprintf(&b, "%-12s %-9s %6s  %-4s  %s\n", "ID", "SOURCE", "AGE", "STEP", "WHAT")
	for _, row := range snap.rows {
		fmt.Fprintf(&b, "%-12s %-9s %6s  %-4d  %s\n",
			row.ID, approvalSourceLabel(row.SourceKind),
			jobs.ShortDuration(approvalAge(row.Pending, now)), row.Step, row.Title)
		fmt.Fprintf(&b, "    %s\n", ui.Dim(row.Cwd))
		if !row.ready.Actionable {
			fmt.Fprintf(&b, "    %s\n", ui.Warning("not actionable here: "+row.ready.Reason))
		}
	}
	_, _ = fmt.Fprint(out, b.String())
	_, _ = fmt.Fprintln(out, ui.Dim("Run 'camp approvals' in a terminal to approve or reject, or 'camp approvals show <id>' to read one."))
}

// findApproval loads the inbox and selects one request.
func findApproval(ctx context.Context, selector string) (approvalsSnapshot, approvalRow, error) {
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return approvalsSnapshot{}, approvalRow{}, camperrors.Wrap(err, "not in a camp")
	}
	pending, err := approvalkit.Load(ctx, campRoot)
	if err != nil {
		return approvalsSnapshot{}, approvalRow{}, camperrors.Wrap(err, "reading approvals")
	}
	p, err := approvalkit.Find(pending, selector)
	if err != nil {
		return approvalsSnapshot{}, approvalRow{}, err
	}
	row := approvalRow{Pending: p, ready: approvals.Check(ctx, campRoot, p)}
	return approvalsSnapshot{campRoot: campRoot}, row, nil
}

func runApprovalsShow(cmd *cobra.Command, args []string) error {
	_, row, err := findApproval(cmd.Context(), args[0])
	if err != nil {
		return err
	}
	if approvalsOpts.json {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			SchemaVersion string       `json:"schema_version"`
			Approval      approvalJSON `json:"approval"`
		}{ApprovalsJSONVersion, approvalToJSON(row, time.Now())})
	}
	_, _ = fmt.Fprint(cmd.OutOrStdout(), strings.Join(approvalDetailLines(row, time.Now(), 0), "\n")+"\n")
	return nil
}

// approvalDetailLines renders one request in full. width wraps the body; zero
// leaves it unwrapped.
func approvalDetailLines(row approvalRow, now time.Time, width int) []string {
	who := row.RequestedBy.Name
	if who == "" {
		who = string(row.RequestedBy.Type)
	} else {
		who += " (" + string(row.RequestedBy.Type) + ")"
	}
	lines := []string{
		ui.BoldText(row.Title),
		"",
		ui.KeyValue("ID", row.ID),
		ui.KeyValue("Source", fmt.Sprintf("%s, step %d %s", approvalSourceLabel(row.SourceKind), row.Step, row.StepName)),
		ui.KeyValue("Where", row.Cwd),
		ui.KeyValue("Requested", fmt.Sprintf("%s ago by %s", jobs.ShortDuration(approvalAge(row.Pending, now)), who)),
	}
	if row.Writer != "" {
		lines = append(lines, ui.KeyValue("Machine", row.Writer))
	}
	if row.ready.Actionable {
		if argv, err := approvals.Command(row.Pending, approvals.Approve, ""); err == nil {
			lines = append(lines, ui.KeyValue("To approve", strings.Join(argv, " ")+"   (in "+row.Cwd+", from a terminal)"))
		}
	} else {
		lines = append(lines, ui.Warning("Not actionable here: "+row.ready.Reason))
	}
	if body := strings.TrimSpace(row.Body); body != "" {
		lines = append(lines, "")
		for _, line := range strings.Split(body, "\n") {
			lines = append(lines, wrapApprovalText(line, width)...)
		}
	}
	return lines
}

// wrapApprovalText wraps one line of request text at word boundaries, keeping
// its leading indent on continuation lines. width <= 0 means no wrapping.
func wrapApprovalText(line string, width int) []string {
	if width <= 0 || len([]rune(line)) <= width {
		return []string{line}
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
	var out []string
	current := indent
	for _, word := range strings.Fields(line) {
		candidate := current + word
		if current != indent {
			candidate = current + " " + word
		}
		if len([]rune(candidate)) > width && current != indent {
			out = append(out, current)
			current = indent + word
			continue
		}
		current = candidate
	}
	return append(out, current)
}

func runApprovalsPath(cmd *cobra.Command, args []string) error {
	snap, row, err := findApproval(cmd.Context(), args[0])
	if err != nil {
		return err
	}
	dir, err := approvalkit.ResolveCwd(snap.campRoot, row.Cwd)
	if err != nil {
		return camperrors.Wrap(err, "the source directory of "+row.ID+" is not available on this machine")
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), dir)
	return nil
}

func runApprovalsWithdraw(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if approvalsOpts.stale {
		if len(args) > 0 {
			return camperrors.New("pass an id or --stale, not both")
		}
		return runApprovalsWithdrawStale(cmd)
	}
	if len(args) != 1 {
		return camperrors.New("name the request to withdraw, or pass --stale")
	}
	reason := strings.TrimSpace(approvalsOpts.reason)
	if reason == "" {
		return camperrors.New("--reason is required")
	}
	snap, row, err := findApproval(ctx, args[0])
	if err != nil {
		return err
	}
	if err := withdrawApproval(ctx, cmd, snap.campRoot, row.Pending, reason); err != nil {
		return err
	}
	if note := commitApprovals(ctx, snap.campRoot); note != "" {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), note)
	}
	return nil
}

// withdrawApproval records that one request is no longer waiting. It resolves
// that request (and the duplicates shown with it) and nothing else: another
// request from the same source, which may be live, stays pending.
func withdrawApproval(ctx context.Context, cmd *cobra.Command, campRoot string, p approvalkit.Pending, reason string) error {
	writerID, err := ledgerkit.ResolveWriterID(ctx)
	if err != nil {
		return camperrors.Wrap(err, "resolving this machine's ledger writer id")
	}
	rec, err := approvalkit.NewRecorder(campRoot, writerID, ledger.ResolveActor(ctx))
	if err != nil {
		return err
	}
	resolved, err := rec.Resolve(ctx, p, approvalkit.OutcomeWithdrawn, reason)
	if err != nil {
		return camperrors.Wrap(err, "recording the withdrawal")
	}
	if !resolved {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is no longer pending\n", p.ID)
		return nil
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "withdrew %s: %s\n", p.ID, p.Title)
	return nil
}

// approvalIsStale reports whether a request can never be decided: its source,
// read on this machine, has finished or is past the recorded step. A source
// that is behind the step, cannot be read here, or is of an unknown kind is not
// stale: camp cannot tell from this machine, and a withdrawal is shared with
// every other one.
func approvalIsStale(r approvals.Readiness) bool {
	return !r.Actionable && r.Over
}

func runApprovalsWithdrawStale(cmd *cobra.Command) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp")
	}
	snap, err := loadApprovals(ctx, campRoot)
	if err != nil {
		return err
	}
	withdrawn := 0
	for _, row := range snap.rows {
		if !approvalIsStale(row.ready) {
			continue
		}
		if err := withdrawApproval(ctx, cmd, campRoot, row.Pending, "source reports "+row.ready.Reason); err != nil {
			return err
		}
		withdrawn++
	}
	if withdrawn == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no stale requests: nothing here has a source that has finished or moved past its step")
		return nil
	}
	if note := commitApprovals(ctx, campRoot); note != "" {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), note)
	}
	return nil
}

func runApprovalsCommit(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp")
	}
	res, err := approvalkit.Commit(ctx, campRoot)
	if err != nil {
		return err
	}
	if res.Status != approvalkit.CommitCreated {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "nothing to commit: "+res.Reason)
		return nil
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "committed approval events ("+shortApprovalSHA(res.Hash)+")")
	return nil
}

func shortApprovalSHA(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}
