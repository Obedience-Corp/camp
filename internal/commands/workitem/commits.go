package workitem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

const (
	commitsDefaultLimit = 100
	commitsLogFormat    = "%H%x09%an%x09%aI%x09%s"
	commitsMaxWorkers   = 8
)

var commitsPerRepoTimeout = 30 * time.Second

// CommitRecord is the per-row payload emitted by `camp workitem commits`.
// Repo is the campaign-relative path of the git repo the commit was found
// in, or "campaign-root" for the campaign root itself.
type CommitRecord struct {
	SHA      string                  `json:"sha"`
	Author   string                  `json:"author"`
	Date     time.Time               `json:"date"`
	Subject  string                  `json:"subject"`
	Repo     string                  `json:"repo"`
	TagParts commitkit.TagComponents `json:"tag,omitempty"`
}

// commitsQueryError records a per-repo failure surfaced under the --json
// `errors` key. Table output emits a stderr warning when this list is non-empty.
type commitsQueryError struct {
	Repo string `json:"repo"`
	Err  string `json:"error"`
}

func newCommitsCommand() *cobra.Command {
	var (
		flagRef      string
		flagJSON     bool
		flagLimit    int
		flagOffset   int
		flagWorkitem string
		flagSource   string
	)

	cmd := &cobra.Command{
		Use:   "commits [selector]",
		Short: "List commits referencing a workitem",
		Long: `List commits referencing this workitem, newest first.

When the campaign event ledger already holds the workitem's commit evidence,
the answer comes from a single merged ledger read (fast path). Otherwise it
falls back to scanning the campaign root and every linked
project/repo/worktree/festival repo for commits whose campaign tag references
the workitem's ref (pre-ledger history).

Use --json for structured output; the "source" field reports which path
answered ("ledger" or "scan"). Repos that are not git checkouts or that fail
their git log invocation are reported under "errors" in JSON mode; table mode
warns on stderr when repo queries fail.`,
		Args: jsoncontract.Args(WorkitemCommitsJSONVersion, func() bool { return flagJSON }, cobra.MaximumNArgs(1)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only query with --json output for automation",
		},
		RunE: jsoncontract.RunE(WorkitemCommitsJSONVersion, func() bool { return flagJSON }, func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			if flagWorkitem != "" {
				selector = flagWorkitem
			}
			return runCommitsQuery(ctx, cmd, commitsFlags{
				Selector: selector,
				Ref:      flagRef,
				JSON:     flagJSON,
				Limit:    flagLimit,
				Offset:   flagOffset,
				Source:   flagSource,
			})
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemCommitsJSONVersion, func() bool { return flagJSON }))
	cmd.Flags().StringVar(&flagRef, "ref", "", "query by workitem ref directly (e.g. WI-abc123) — skips resolver")
	cmd.Flags().StringVar(&flagWorkitem, "workitem", "", "alias for the positional <selector>")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit JSON instead of the default table")
	cmd.Flags().IntVar(&flagLimit, "limit", commitsDefaultLimit, "maximum commits to return")
	cmd.Flags().IntVar(&flagOffset, "offset", 0, "number of commits to skip (after sorting)")
	cmd.Flags().StringVar(&flagSource, "source", commitsSourceAuto, "where to read commits from: auto (ledger when present, else scan), ledger, or scan")
	return cmd
}

const (
	commitsSourceAuto   = "auto"
	commitsSourceLedger = "ledger"
	commitsSourceScan   = "scan"
)

type commitsFlags struct {
	Selector string
	Ref      string
	JSON     bool
	Limit    int
	Offset   int
	Source   string
}

func runCommitsQuery(ctx context.Context, cmd *cobra.Command, flags commitsFlags) error {
	_, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	ref := flags.Ref
	var wi *wkitem.WorkItem
	if ref == "" {
		res, rerr := resolver.Resolve(ctx, campaignRoot, resolver.Options{Explicit: flags.Selector})
		if rerr != nil {
			return camperrors.Wrap(rerr, "resolve workitem")
		}
		if res == nil || res.Workitem == nil {
			return camperrors.NewValidation("workitem",
				"no workitem context resolved; pass <selector> or --ref WI-...", nil)
		}
		wi = res.Workitem
		ref = wkitem.RefOf(wi)
		if ref == "" {
			return camperrors.NewValidation("workitem",
				"workitem has no ref; run `camp workitem doctor --fix` to backfill", nil)
		}
	} else {
		// --ref skips the full resolve for the identity, but still expand D007
		// aliases (stable id / key / slug) when the ref maps to an on-disk workitem.
		if res, rerr := resolver.Resolve(ctx, campaignRoot, resolver.Options{Explicit: ref}); rerr == nil && res != nil && res.Workitem != nil {
			wi = res.Workitem
			if r := wkitem.RefOf(wi); r != "" {
				ref = r
			}
		}
	}

	requested := flags.Source
	if requested == "" {
		requested = commitsSourceAuto
	}
	if requested != commitsSourceAuto && requested != commitsSourceLedger && requested != commitsSourceScan {
		return camperrors.NewValidation("source",
			"must be auto, ledger, or scan", nil)
	}

	// Ledger-first (D007/A6): when the campaign event ledger holds this
	// workitem's commit evidence, answer from one merged ledger read instead of a
	// git-log fan-out across every linked repo. In auto, an empty or unreadable
	// ledger falls back to the cross-repo tag scan; --source scan forces the scan
	// (the exhaustive git --all view) and --source ledger forces the ledger.
	aliases := workitemAliases(ref, wi)
	var records []CommitRecord
	var queryErrs []commitsQueryError
	answered := commitsSourceScan
	if requested == commitsSourceLedger || requested == commitsSourceAuto {
		ledgerRecs, lerr := ledgerCommits(ctx, campaignRoot, aliases)
		if requested == commitsSourceLedger {
			if lerr != nil {
				return camperrors.Wrap(lerr, "read campaign ledger")
			}
			records, answered = ledgerRecs, commitsSourceLedger
		} else if lerr == nil && len(ledgerRecs) > 0 {
			records, answered = ledgerRecs, commitsSourceLedger
		} else if lerr != nil {
			// Empty ledger → silent scan is fine; unreadable/corrupt is loud.
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: campaign ledger unreadable (%v); falling back to scan\n", lerr)
		}
	}
	if answered == commitsSourceScan && requested != commitsSourceLedger {
		repos, rerr := enumerateQueryRepos(ctx, campaignRoot)
		if rerr != nil {
			return camperrors.Wrap(rerr, "enumerate query repos")
		}
		records, queryErrs = searchRepos(ctx, campaignRoot, repos, ref)
	}
	source := answered
	sort.Slice(records, func(i, j int) bool { return records[i].Date.After(records[j].Date) })

	if flags.Offset > 0 && flags.Offset < len(records) {
		records = records[flags.Offset:]
	} else if flags.Offset >= len(records) {
		records = nil
	}
	if flags.Limit > 0 && flags.Limit < len(records) {
		records = records[:flags.Limit]
	}

	if flags.JSON {
		return emitCommitsJSON(cmd.OutOrStdout(), source, records, queryErrs)
	}
	if err := emitCommitsTable(cmd.OutOrStdout(), source, records); err != nil {
		return err
	}
	return emitCommitsQueryWarnings(cmd.ErrOrStderr(), queryErrs)
}

// workitemAliases returns the identifier forms a ledger event's scope.workitem
// might carry for this workitem (D007 workitem-id normalization): the ref, the
// stable id, the workitem key, and the on-disk directory slug all name it.
func workitemAliases(ref string, wi *wkitem.WorkItem) map[string]bool {
	aliases := map[string]bool{}
	if ref != "" {
		aliases[ref] = true
	}
	if wi != nil {
		if wi.StableID != "" {
			aliases[wi.StableID] = true
		}
		if wi.Key != "" {
			aliases[wi.Key] = true
		}
		if slug := filepath.Base(filepath.FromSlash(wi.RelativePath)); slug != "" && slug != "." {
			aliases[slug] = true
		}
	}
	return aliases
}

// ledgerCommits answers the query from the campaign event ledger: the commit
// evidence on every evidence_attached/repaired event whose scope.workitem names
// this workitem. A missing or empty ledger yields no records so the caller
// falls back to the cross-repo scan.
func ledgerCommits(ctx context.Context, campaignRoot string, aliases map[string]bool) ([]CommitRecord, error) {
	if len(aliases) == 0 {
		return nil, nil
	}
	reader, err := ledgerkit.NewReader(campaignRoot)
	if err != nil {
		return nil, err
	}
	events, _, err := reader.Query(ctx, ledgerkit.Filter{
		Kinds: []ledgerkit.Kind{ledgerkit.KindEvidenceAttached, ledgerkit.KindRepaired},
	})
	if err != nil {
		return nil, err
	}
	return commitsFromLedgerEvents(events, aliases), nil
}

// commitsFromLedgerEvents maps commit evidence to CommitRecords, matching the
// git-log path's record shape exactly (tag parsed from the subject) so the
// --json commit contract is identical whichever path answered. Pure so the
// mapping is unit-testable without a ledger on disk.
func commitsFromLedgerEvents(events []*ledgerkit.Event, aliases map[string]bool) []CommitRecord {
	var records []CommitRecord
	seen := map[string]bool{}
	for _, ev := range events {
		if !aliases[ev.Scope.Workitem] {
			continue
		}
		for _, e := range ev.Evidence {
			if e.Type != ledgerkit.EvidenceCommit || e.SHA == "" {
				continue
			}
			key := e.Repo + "\x00" + e.SHA
			if seen[key] {
				continue
			}
			seen[key] = true
			records = append(records, CommitRecord{
				SHA:      e.SHA,
				Author:   ledgerCommitAuthor(ev),
				Date:     ledgerCommitDate(ev),
				Subject:  ev.Why,
				Repo:     e.Repo,
				TagParts: commitkit.ParseTag(ev.Why),
			})
		}
	}
	return records
}

func ledgerCommitAuthor(ev *ledgerkit.Event) string {
	if ev.Payload != nil {
		if a, ok := ev.Payload["author"].(string); ok && a != "" {
			return a
		}
	}
	return ev.Actor.Name
}

// ledgerCommitDate prefers payload commit_date/date (live capture + backfill)
// and falls back to the event timestamp.
func ledgerCommitDate(ev *ledgerkit.Event) time.Time {
	if ev.Payload != nil {
		for _, key := range []string{"commit_date", "date"} {
			if d, ok := ev.Payload[key].(string); ok && d != "" {
				if t := parseLedgerTS(d); !t.IsZero() {
					return t
				}
			}
		}
	}
	return parseLedgerTS(ev.TS)
}

func parseLedgerTS(ts string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse("2006-01-02", ts); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// enumerateQueryRepos returns absolute paths of every git repo to search,
// deduplicated by canonical path. Always includes the campaign root.
func enumerateQueryRepos(ctx context.Context, campaignRoot string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{campaignRoot}
	seen[campaignRoot] = true

	registry, err := links.Load(ctx, campaignRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "load link registry")
	}
	for i := range registry.Links {
		link := &registry.Links[i]
		switch link.Scope.Kind {
		case links.ScopeProject, links.ScopeRepo, links.ScopeWorktree:
			abs := filepath.Join(campaignRoot, filepath.FromSlash(link.Scope.Path))
			if seen[abs] {
				continue
			}
			seen[abs] = true
			out = append(out, abs)
		case links.ScopeFestival:
			// Festival scope refers to a festival directory under the campaign
			// root, not a separate repo. The campaign root entry already
			// covers that path.
		}
	}
	return out, nil
}

// searchRepos fans out across repos with a bounded worker pool and gathers
// the matched commits + per-repo errors.
func searchRepos(ctx context.Context, campaignRoot string, repos []string, ref string) ([]CommitRecord, []commitsQueryError) {
	workers := commitsWorkerCount(len(repos))

	type job struct{ repo string }
	jobs := make(chan job, len(repos))
	type out struct {
		records []CommitRecord
		err     *commitsQueryError
	}
	results := make(chan out, len(repos))

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				records, err := queryRepo(ctx, campaignRoot, j.repo, ref)
				if err != nil {
					results <- out{err: &commitsQueryError{Repo: repoDisplayPath(campaignRoot, j.repo), Err: err.Error()}}
					continue
				}
				results <- out{records: records}
			}
		}()
	}
	for _, repo := range repos {
		jobs <- job{repo: repo}
	}
	close(jobs)
	wg.Wait()
	close(results)

	var all []CommitRecord
	var errs []commitsQueryError
	for r := range results {
		all = append(all, r.records...)
		if r.err != nil {
			errs = append(errs, *r.err)
		}
	}
	sort.Slice(errs, func(i, j int) bool { return errs[i].Repo < errs[j].Repo })
	return all, errs
}

func commitsWorkerCount(repoCount int) int {
	if repoCount < 1 {
		return 1
	}
	workers := runtime.NumCPU()
	if workers > commitsMaxWorkers {
		workers = commitsMaxWorkers
	}
	if workers > repoCount {
		workers = repoCount
	}
	if workers < 1 {
		workers = 1
	}
	return workers
}

// queryRepo runs `git log` in repo, parses the tab-separated output, and
// filters out commits whose parsed tag does not match ref exactly. Returns
// nil records (not an error) when the directory is not a git repo so the
// caller can skip silently.
func queryRepo(ctx context.Context, campaignRoot, repo, ref string) ([]CommitRecord, error) {
	cctx, cancel := context.WithTimeout(ctx, commitsPerRepoTimeout)
	defer cancel()

	ok, err := isGitRepo(cctx, repo)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	// The ref already starts with WI-, so "-"+ref anchors on the segment
	// separator and matches both the single-prefix (-WI-abc123) and the
	// historical doubled (-WI-WI-abc123) subject forms.
	grep := "-" + ref
	cmd := exec.CommandContext(cctx, "git",
		"-C", repo,
		"log", "--all",
		"--pretty=format:"+commitsLogFormat,
		"--grep="+grep,
	)
	output, err := cmd.Output()
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return nil, camperrors.New(fmt.Sprintf("git log timeout after %s", commitsPerRepoTimeout))
	}
	if err != nil {
		return nil, err
	}
	rel := repoDisplayPath(campaignRoot, repo)
	var records []CommitRecord
	for _, line := range strings.Split(strings.TrimRight(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) < 4 {
			continue
		}
		date, _ := time.Parse(time.RFC3339, fields[2])
		subject := fields[3]
		parts := commitkit.ParseTag(subject)
		if parts.WorkitemRef != ref {
			continue
		}
		records = append(records, CommitRecord{
			SHA:      fields[0],
			Author:   fields[1],
			Date:     date,
			Subject:  subject,
			Repo:     rel,
			TagParts: parts,
		})
	}
	return records, nil
}

// repoDisplayPath must label the campaign root the same way the ledger scan
// path does (ledger.RepoLabel: "campaign-root") so records read the same
// shape regardless of which path (ledger fast-path vs. this git-log scan)
// answered the query.
func repoDisplayPath(campaignRoot, repo string) string {
	rel, err := filepath.Rel(campaignRoot, repo)
	if err == nil {
		slashRel := filepath.ToSlash(rel)
		if slashRel == "." {
			return "campaign-root"
		}
		if slashRel != ".." && !strings.HasPrefix(slashRel, "../") && !filepath.IsAbs(rel) {
			return slashRel
		}
	}
	return filepath.Base(repo)
}

func isGitRepo(ctx context.Context, path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--git-dir")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			msg := strings.ToLower(string(output))
			if strings.Contains(msg, "not a git repository") {
				return false, nil
			}
			return false, camperrors.New(fmt.Sprintf("git rev-parse failed: %s", strings.TrimSpace(string(output))))
		}
		return false, err
	}
	return true, nil
}

func emitCommitsTable(w io.Writer, source string, records []CommitRecord) error {
	if source != "" {
		if _, err := fmt.Fprintf(w, "source: %s\n", source); err != nil {
			return err
		}
	}
	if len(records) == 0 {
		_, err := fmt.Fprintln(w, "no commits found")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "REPO\tSHA\tDATE\tSUBJECT"); err != nil {
		return err
	}
	for _, r := range records {
		sha := r.SHA
		if len(sha) > 8 {
			sha = sha[:8]
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			r.Repo, sha, r.Date.UTC().Format("2006-01-02"), r.Subject); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func emitCommitsQueryWarnings(w io.Writer, errs []commitsQueryError) error {
	if len(errs) == 0 {
		return nil
	}
	_, err := fmt.Fprintf(w, "warning: %d repo(s) failed; re-run with --json for details\n", len(errs))
	return err
}

// WorkitemCommitsJSONVersion is declared in json_contract.go alongside the
// rest of the agent-facing JSON schema versions.

func emitCommitsJSON(w io.Writer, source string, records []CommitRecord, errs []commitsQueryError) error {
	if records == nil {
		records = []CommitRecord{}
	}
	payload := struct {
		SchemaVersion string              `json:"schema_version"`
		Source        string              `json:"source"`
		Commits       []CommitRecord      `json:"commits"`
		Errors        []commitsQueryError `json:"errors,omitempty"`
	}{
		SchemaVersion: WorkitemCommitsJSONVersion,
		Source:        source,
		Commits:       records,
		Errors:        errs,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}
