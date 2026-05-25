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
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

const (
	commitsDefaultLimit = 100
	commitsLogFormat    = "%H%x09%an%x09%aI%x09%s"
	commitsMaxWorkers   = 8
)

var commitsPerRepoTimeout = 30 * time.Second

// CommitRecord is the per-row payload emitted by `camp workitem commits`.
// Repo is the campaign-relative path of the git repo the commit was found
// in (".") for the campaign root.
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
	)

	cmd := &cobra.Command{
		Use:   "commits [selector]",
		Short: "List commits referencing a workitem across linked repos",
		Long: `Search the campaign root and every linked project/repo/worktree/festival
repo for commits whose campaign tag references this workitem's ref.

Default sort: most recent first across all repos. Use --json for structured
output. Repos that are not git checkouts or that fail their git log invocation
are reported under "errors" in JSON mode; table mode warns on stderr when
repo queries fail.`,
		Args: cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only query with --json output for automation",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
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
			})
		},
	}
	cmd.Flags().StringVar(&flagRef, "ref", "", "query by workitem ref directly (e.g. WI-abc123) — skips resolver")
	cmd.Flags().StringVar(&flagWorkitem, "workitem", "", "alias for the positional <selector>")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit JSON instead of the default table")
	cmd.Flags().IntVar(&flagLimit, "limit", commitsDefaultLimit, "maximum commits to return")
	cmd.Flags().IntVar(&flagOffset, "offset", 0, "number of commits to skip (after sorting)")
	return cmd
}

type commitsFlags struct {
	Selector string
	Ref      string
	JSON     bool
	Limit    int
	Offset   int
}

func runCommitsQuery(ctx context.Context, cmd *cobra.Command, flags commitsFlags) error {
	_, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	ref := flags.Ref
	if ref == "" {
		res, rerr := resolver.Resolve(ctx, campaignRoot, resolver.Options{Explicit: flags.Selector})
		if rerr != nil {
			return camperrors.Wrap(rerr, "resolve workitem")
		}
		if res == nil || res.Workitem == nil {
			return camperrors.NewValidation("workitem",
				"no workitem context resolved; pass <selector> or --ref WI-...", nil)
		}
		ref = refOf(res.Workitem)
		if ref == "" {
			return camperrors.NewValidation("workitem",
				"workitem has no ref; run `camp workitem doctor --fix` to backfill", nil)
		}
	}

	repos, err := enumerateQueryRepos(ctx, campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "enumerate query repos")
	}

	records, queryErrs := searchRepos(ctx, repos, ref)
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
		return emitCommitsJSON(cmd.OutOrStdout(), records, queryErrs)
	}
	if err := emitCommitsTable(cmd.OutOrStdout(), records); err != nil {
		return err
	}
	emitCommitsQueryWarnings(cmd.ErrOrStderr(), queryErrs)
	return nil
}

// enumerateQueryRepos returns absolute paths of every git repo to search,
// deduplicated by canonical path. Always includes the campaign root.
func enumerateQueryRepos(ctx context.Context, campaignRoot string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{campaignRoot}
	seen[campaignRoot] = true

	registry, err := links.Load(ctx, campaignRoot)
	if err != nil {
		return out, nil // links.yaml may not exist yet; campaign root is enough
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
func searchRepos(ctx context.Context, repos []string, ref string) ([]CommitRecord, []commitsQueryError) {
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
				records, err := queryRepo(ctx, j.repo, ref)
				if err != nil {
					results <- out{err: &commitsQueryError{Repo: j.repo, Err: err.Error()}}
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
func queryRepo(ctx context.Context, repo, ref string) ([]CommitRecord, error) {
	cctx, cancel := context.WithTimeout(ctx, commitsPerRepoTimeout)
	defer cancel()

	ok, err := isGitRepo(cctx, repo)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	grep := "-WI-" + ref
	cmd := exec.CommandContext(cctx, "git",
		"-C", repo,
		"log", "--all",
		"--pretty=format:"+commitsLogFormat,
		"--grep="+grep,
	)
	output, err := cmd.Output()
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("git log timeout after %s", commitsPerRepoTimeout)
	}
	if err != nil {
		return nil, err
	}
	rel := filepath.Base(repo)
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
			return false, fmt.Errorf("git rev-parse failed: %s", strings.TrimSpace(string(output)))
		}
		return false, err
	}
	return true, nil
}

func emitCommitsTable(w io.Writer, records []CommitRecord) error {
	if len(records) == 0 {
		fmt.Fprintln(w, "no commits found")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "REPO\tSHA\tDATE\tSUBJECT")
	for _, r := range records {
		sha := r.SHA
		if len(sha) > 8 {
			sha = sha[:8]
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			r.Repo, sha, r.Date.UTC().Format("2006-01-02"), r.Subject)
	}
	return tw.Flush()
}

func emitCommitsQueryWarnings(w io.Writer, errs []commitsQueryError) {
	if len(errs) == 0 {
		return
	}
	fmt.Fprintf(w, "warning: %d repo(s) failed; re-run with --json for details\n", len(errs))
}

func emitCommitsJSON(w io.Writer, records []CommitRecord, errs []commitsQueryError) error {
	payload := struct {
		Commits []CommitRecord      `json:"commits"`
		Errors  []commitsQueryError `json:"errors,omitempty"`
	}{
		Commits: records,
		Errors:  errs,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}
