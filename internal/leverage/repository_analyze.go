package leverage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// AnalyzeRepositories is read-only; legacy snapshots and blame caches are never consulted.
func AnalyzeRepositories(ctx context.Context, projects []ResolvedProject, runner ScopedRunner, resolver *AuthorResolver, threshold float64) (*RepositoryReport, error) {
	if resolver == nil {
		resolver = NewAuthorResolver(nil)
	}
	r := newRepositoryReport()
	authors := map[string]*Contributor{}
	counted := map[string]bool{}
	for _, project := range projects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := analyzeRepository(ctx, r, project, runner, resolver, authors, counted); err != nil {
			return nil, err
		}
	}
	// Effort spans are repository-wide, including a selected repository where
	// an identity has history but no surviving lines. Ownership only allocates PM.
	for _, p := range r.Projects {
		for id, a := range authors {
			if p.Shallow {
				a.HistoryComplete = false
			}
			if sp, ok := p.spans[id]; ok {
				mergeSpan(a, sp, !p.Shallow)
			}
		}
	}
	r.finish(authors, threshold)
	return r, nil
}

func analyzeRepository(ctx context.Context, r *RepositoryReport, project ResolvedProject, runner ScopedRunner, resolver *AuthorResolver, authors map[string]*Contributor, counted map[string]bool) error {
	dir, err := filepath.Abs(project.SCCDir)
	if err != nil {
		return camperrors.Wrap(err, "resolving target")
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return camperrors.Wrap(err, "directory not found")
	}
	root, err := reportGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return camperrors.Wrap(err, "a local Git working tree is required")
	}
	root = strings.TrimSpace(root)
	before, err := repositoryState(ctx, root)
	if err != nil {
		return err
	}
	initialConfigs, err := scanConfigSources(ctx, dir, nil)
	if err != nil {
		return err
	}
	scan, err := runner.Scan(ctx, dir, project.ExcludeDirs)
	if err != nil {
		return err
	}
	content, err := fingerprint(ctx, dir, scan.Files)
	if err != nil {
		return err
	}
	configs, err := scanConfigSources(ctx, dir, scan.Files)
	if err != nil {
		return err
	}
	// The first scan discovers paths. Hash their bytes before the scoring scan
	// so same-length edits between counting and blame cannot silently mix inputs.
	verified, err := runner.Scan(ctx, dir, project.ExcludeDirs)
	if err != nil {
		return err
	}
	if verified.ManifestDigest != scan.ManifestDigest {
		return camperrors.New("input_changed: source scope changed during scan; rerun the report")
	}
	scan = verified
	p := ReportProject{ID: dir, Target: dir, GitRoot: root, Head: before.head, Dirty: before.status != "", Shallow: before.shallow, RefsDigest: digestJSON(before.refs), ManifestDigest: scan.ManifestDigest, ContentDigest: content, IdentityDigest: digestJSON(resolver.config), ConfigSources: configs, ScanArguments: scan.Arguments, result: scan.Result}
	p.Files = SumFiles(scan.Result)
	p.Lines, p.CodeLines = SumLines(scan.Result)
	p.EstimatedPM = scan.Result.EstimatedPeople * scan.Result.EstimatedScheduleMonths
	p.spans, err = reportAuthorSpans(ctx, root, resolver)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.addDiagnostic("history_unavailable", dir, "", err.Error())
		p.spans = map[string]reportSpan{}
	}
	if before.shallow {
		r.addDiagnostic("shallow_history", dir, "", "history is shallow; contributor effort and leverage are unavailable")
	}
	if !before.shallow && before.head != "" {
		first, last, dateErr := GitDateRange(ctx, root)
		if dateErr == nil {
			months := ElapsedMonths(first, last)
			if months < minAuthorMonths {
				months = minAuthorMonths
			}
			p.teamPM = number(months)
		}
	}
	headFiles := map[string]bool{}
	if before.head != "" {
		files, err := reportGit(ctx, root, "ls-tree", "-r", "-z", "--name-only", "HEAD")
		if err != nil {
			return err
		}
		for _, f := range strings.Split(files, "\x00") {
			headFiles[f] = true
		}
	}
	for _, f := range scan.Files {
		full := filepath.Join(dir, filepath.FromSlash(f.Location))
		if counted[full] {
			return camperrors.Newf("overlapping project file: %s", full)
		}
		counted[full] = true
	}
	results, err := collectManifestBlame(ctx, dir, root, scan.Files, headFiles)
	if err != nil {
		return err
	}
	for _, result := range results {
		if result.err != nil {
			r.addDiagnostic("blame_unavailable", dir, result.path, result.err.Error())
		}
		for key, lines := range result.buckets {
			b := r.Summary.Buckets[key]
			b.Lines += lines
			if p.Lines > 0 {
				b.EstimatedPM += p.EstimatedPM * float64(lines) / float64(p.Lines)
			}
		}
		for email, ownership := range result.owners {
			id := resolver.Resolve(email)
			bucket := "contributors"
			if resolver.IsExcluded(email) {
				bucket = "excluded_identities"
			}
			b := r.Summary.Buckets[bucket]
			b.Lines += ownership.lines
			allocation := 0.0
			if p.Lines > 0 {
				allocation = p.EstimatedPM * float64(ownership.lines) / float64(p.Lines)
			}
			b.EstimatedPM += allocation
			if bucket != "contributors" {
				continue
			}
			a := authors[id]
			if a == nil {
				a = &Contributor{ID: id, HistoryComplete: true}
				authors[id] = a
			}
			a.Lines += ownership.lines
			a.EstimatedPM += allocation
			sp, ok := p.spans[id]
			mergeSpan(a, sp, ok && !before.shallow)
			if configured := resolver.DisplayName(id); configured != id {
				a.Name = configured
			} else if a.Name == "" {
				a.Name = ownership.name
			}
			if !ok {
				r.addDiagnostic("author_history_unavailable", dir, result.path, "no complete commit span for "+id)
			}
		}
	}
	after, err := repositoryState(ctx, root)
	if err != nil {
		return err
	}
	finalContent, err := fingerprint(ctx, dir, scan.Files)
	if err != nil {
		return camperrors.Wrap(err, "input_changed: rerun the report")
	}
	finalConfig, err := scanConfigSources(ctx, dir, scan.Files)
	if err != nil {
		return err
	}
	rootConfigs, err := scanConfigSources(ctx, dir, nil)
	if err != nil {
		return err
	}
	if before != after || content != finalContent || digestJSON(configs) != digestJSON(finalConfig) || digestJSON(initialConfigs) != digestJSON(rootConfigs) {
		return camperrors.New("input_changed: repository changed during analysis; rerun the report")
	}
	r.Projects = append(r.Projects, p)
	r.Summary.Files += p.Files
	r.Summary.Lines += p.Lines
	r.Summary.CodeLines += p.CodeLines
	r.Summary.EstimatedPM += p.EstimatedPM
	r.Tools["scc"] = scan.Version
	gitVersion, err := reportGit(ctx, root, "--version")
	if err != nil {
		return err
	}
	r.Tools["git"] = strings.TrimSpace(gitVersion)
	return nil
}

type reportRepoState struct {
	head, refs, status string
	shallow            bool
}

func repositoryState(ctx context.Context, root string) (reportRepoState, error) {
	var s reportRepoState
	head, err := reportGit(ctx, root, "rev-parse", "--verify", "HEAD")
	if err == nil {
		s.head = strings.TrimSpace(head)
	} else {
		// An unborn symbolic HEAD is valid; a broken detached HEAD is not.
		if _, symErr := reportGit(ctx, root, "symbolic-ref", "-q", "HEAD"); symErr != nil {
			return s, err
		}
	}
	s.refs, err = reportGit(ctx, root, "for-each-ref", "--format=%(refname) %(objectname)")
	if err != nil {
		return s, err
	}
	s.status, err = reportGit(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return s, err
	}
	shallow, err := reportGit(ctx, root, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return s, err
	}
	s.shallow = strings.TrimSpace(shallow) == "true"
	return s, nil
}

func reportGit(ctx context.Context, dir string, args ...string) (string, error) {
	argv := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", camperrors.Wrapf(err, "git %s", args[0])
	}
	return string(out), nil
}

func reportAuthorSpans(ctx context.Context, root string, resolver *AuthorResolver) (map[string]reportSpan, error) {
	out, err := reportGit(ctx, root, "log", "--all", "-z", "--format=%aE%x00%aN%x00%cI")
	if err != nil {
		return nil, err
	}
	spans := map[string]reportSpan{}
	parts := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if out == "" {
		return spans, nil
	}
	if len(parts)%3 != 0 {
		return nil, camperrors.New("invalid author date output")
	}
	for i := 0; i < len(parts); i += 3 {
		email := strings.TrimSpace(parts[i])
		if resolver.IsExcluded(email) {
			continue
		}
		date, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[i+2]))
		if err != nil {
			return nil, camperrors.Wrap(err, "parsing author date")
		}
		id := resolver.Resolve(email)
		sp := spans[id]
		sp.emails = append(sp.emails, email)
		if sp.first.IsZero() || date.Before(sp.first) {
			sp.first = date
		}
		if sp.last.IsZero() || date.After(sp.last) {
			sp.last = date
			sp.name = parts[i+1]
		}
		spans[id] = sp
	}
	return spans, nil
}

// Record configuration that can affect counted files, including ancestor rules
// and rules in directories containing scanned files. No private contents are exported.
func scanConfigSources(ctx context.Context, dir string, files []ScannedFile) ([]ConfigSource, error) {
	dirs := map[string]bool{dir: true}
	for _, file := range files {
		for current := filepath.Dir(filepath.Join(dir, filepath.FromSlash(file.Location))); ; current = filepath.Dir(current) {
			dirs[current] = true
			if current == dir || current == filepath.Dir(current) {
				break
			}
		}
	}
	if root, err := reportGit(ctx, dir, "rev-parse", "--show-toplevel"); err == nil {
		root = strings.TrimSpace(root)
		for parent := dir; parent != root && parent != filepath.Dir(parent); {
			parent = filepath.Dir(parent)
			dirs[parent] = true
		}
	}
	paths := map[string]bool{}
	for d := range dirs {
		for _, name := range []string{".sccignore", ".gitignore", ".ignore", ".sccconfig", ".mailmap"} {
			paths[filepath.Join(d, name)] = true
		}
	}
	if global := os.Getenv("SCC_CONFIG_PATH"); global != "" {
		paths[global] = true
	}
	sources := []ConfigSource{}
	for path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, camperrors.Wrap(err, "reading scan configuration")
		}
		hash, err := hashFile(path)
		if err != nil {
			return nil, err
		}
		sources = append(sources, ConfigSource{path, hash})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	return sources, nil
}
