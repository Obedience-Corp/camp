package leverage

import (
	"math"
	"sort"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const ContributorReportSchema = "leverage-contributors/v1"
const RepositoryReportSchema = "leverage-repository/v1"

type ReportDiagnostic struct {
	Code      string `json:"code"`
	Severity  string `json:"severity"`
	ProjectID string `json:"project_id"`
	Path      string `json:"path,omitempty"`
	Message   string `json:"message"`
}

type AccountingBucket struct {
	Lines       int     `json:"lines"`
	Share       float64 `json:"share"`
	EstimatedPM float64 `json:"estimated_person_months"`
}

type ReportSummary struct {
	Files       int                          `json:"files"`
	Lines       int                          `json:"lines"`
	CodeLines   int                          `json:"code_lines"`
	EstimatedPM float64                      `json:"estimated_person_months"`
	Coverage    *float64                     `json:"attribution_coverage"`
	Buckets     map[string]*AccountingBucket `json:"buckets"`
}

type Contributor struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Lines           int        `json:"lines"`
	Share           float64    `json:"share"`
	EstimatedPM     float64    `json:"estimated_person_months"`
	First           *time.Time `json:"first_commit_at"`
	Last            *time.Time `json:"last_commit_at"`
	ElapsedPM       *float64   `json:"elapsed_person_months"`
	Leverage        *float64   `json:"leverage"`
	HistoryComplete bool       `json:"history_complete"`
}

type ReportProject struct {
	ID             string         `json:"id"`
	GitRoot        string         `json:"git_root"`
	Target         string         `json:"target"`
	Head           string         `json:"head"`
	Dirty          bool           `json:"dirty"`
	Shallow        bool           `json:"shallow"`
	RefsDigest     string         `json:"refs_digest"`
	ManifestDigest string         `json:"manifest_digest"`
	ContentDigest  string         `json:"content_digest"`
	IdentityDigest string         `json:"identity_digest"`
	Files          int            `json:"files"`
	Lines          int            `json:"lines"`
	CodeLines      int            `json:"code_lines"`
	EstimatedPM    float64        `json:"estimated_person_months"`
	ConfigSources  []ConfigSource `json:"config_sources"`
	ScanArguments  []string       `json:"scan_arguments"`
	result         *SCCResult
	spans          map[string]reportSpan
	teamPM         *float64
}

type ReportDisplay struct {
	Threshold    float64 `json:"threshold_percent"`
	Omitted      int     `json:"omitted_contributors"`
	OmittedLines int     `json:"omitted_lines"`
	OmittedShare float64 `json:"omitted_share"`
}

type HistoryMethod struct {
	ID            string  `json:"id"`
	Refs          string  `json:"refs"`
	DateField     string  `json:"date_field"`
	DaysPerMonth  float64 `json:"days_per_month"`
	MinimumMonths float64 `json:"minimum_months"`
}

type RepositoryReport struct {
	SchemaVersion string             `json:"schema_version"`
	Method        string             `json:"method"`
	GeneratedAt   time.Time          `json:"generated_at"`
	Status        string             `json:"status"`
	Tools         map[string]string  `json:"tools"`
	History       HistoryMethod      `json:"history_method"`
	Projects      []ReportProject    `json:"projects"`
	Summary       ReportSummary      `json:"summary"`
	Display       ReportDisplay      `json:"display"`
	Contributors  []Contributor      `json:"contributors"`
	Diagnostics   []ReportDiagnostic `json:"diagnostics"`
	all           []Contributor
}

type reportSpan struct {
	first, last time.Time
	name        string
	emails      []string
}

func newRepositoryReport() *RepositoryReport {
	return &RepositoryReport{SchemaVersion: ContributorReportSchema, Method: "contributor_worktree_v1", GeneratedAt: time.Now().UTC(), Status: "complete",
		Tools: map[string]string{}, History: HistoryMethod{"repository_commit_span_v1", "all_local_refs", "committer", 30.44, 0.1},
		Projects: []ReportProject{}, Contributors: []Contributor{}, Diagnostics: []ReportDiagnostic{},
		Summary: ReportSummary{Buckets: map[string]*AccountingBucket{"contributors": {}, "excluded_identities": {}, "uncommitted": {}, "unavailable": {}}}}
}

func (r *RepositoryReport) addDiagnostic(code, project, path, message string) {
	r.Diagnostics = append(r.Diagnostics, ReportDiagnostic{code, "warning", project, path, message})
	r.Status = "partial"
}

func (r *RepositoryReport) finish(authors map[string]*Contributor, threshold float64) {
	r.Display = ReportDisplay{Threshold: threshold}
	if r.Summary.Lines > 0 {
		for _, b := range r.Summary.Buckets {
			b.Share = float64(b.Lines) / float64(r.Summary.Lines)
		}
		r.Summary.Coverage = number(1 - r.Summary.Buckets["unavailable"].Share)
	} else if r.Status == "complete" {
		r.Status = "empty"
	}
	for _, a := range authors {
		if r.Summary.Lines > 0 {
			a.Share = float64(a.Lines) / float64(r.Summary.Lines)
		}
		if a.HistoryComplete && a.First != nil && a.Last != nil {
			a.ElapsedPM = number(math.Max(minAuthorMonths, ElapsedMonths(*a.First, *a.Last)))
			a.Leverage = number(a.EstimatedPM / *a.ElapsedPM)
		}
		r.all = append(r.all, *a)
	}
	sort.Slice(r.all, func(i, j int) bool {
		a, b := r.all[i], r.all[j]
		if a.Leverage == nil && b.Leverage != nil {
			return false
		}
		if a.Leverage != nil && b.Leverage == nil {
			return true
		}
		if a.Leverage != nil && b.Leverage != nil && *a.Leverage != *b.Leverage {
			return *a.Leverage > *b.Leverage
		}
		return a.ID < b.ID
	})
	for _, a := range r.all {
		if a.Share*100 < threshold {
			r.Display.Omitted++
			r.Display.OmittedLines += a.Lines
			r.Display.OmittedShare += a.Share
		} else {
			r.Contributors = append(r.Contributors, a)
		}
	}
}

func number(n float64) *float64 { return &n }

// RepositoryScore preserves legacy score keys while making unavailable metrics null.
type RepositoryScore struct {
	ProjectName     string   `json:"project_name,omitempty"`
	EstimatedPeople float64  `json:"estimated_people"`
	EstimatedMonths float64  `json:"estimated_months"`
	EstimatedCost   float64  `json:"estimated_cost"`
	ActualPeople    float64  `json:"actual_people"`
	ElapsedMonths   *float64 `json:"elapsed_months"`
	ActualPM        *float64 `json:"actual_person_months"`
	AuthorCount     int      `json:"author_count"`
	FullLeverage    *float64 `json:"full_leverage"`
	SimpleLeverage  *float64 `json:"simple_leverage"`
	TotalFiles      int      `json:"total_files"`
	TotalLines      int      `json:"total_lines"`
	TotalCode       int      `json:"total_code"`
}

// Score projects a single repository into summary or personal view without rescanning.
func (r *RepositoryReport) Score(selector string, people int, resolver *AuthorResolver) (RepositoryScore, []string, string, error) {
	if len(r.Projects) != 1 {
		return RepositoryScore{}, nil, "", camperrors.New("repository summary requires one target")
	}
	p := r.Projects[0]
	s := RepositoryScore{ProjectName: p.Target, EstimatedPeople: p.result.EstimatedPeople, EstimatedMonths: p.result.EstimatedScheduleMonths, EstimatedCost: p.result.EstimatedCost, TotalFiles: p.Files, TotalLines: p.Lines, TotalCode: p.CodeLines, AuthorCount: len(p.spans), ActualPeople: float64(len(p.spans))}
	matched := []string{}
	method := "repository_blame_weighted_v1"
	estimated := p.EstimatedPM
	if selector != "" {
		method = "repository_commit_span_v1"
		match := ExpandAuthorFilter(resolver, selector)
		var first, last time.Time
		for id, sp := range p.spans {
			ok := false
			for _, email := range sp.emails {
				if match.MatchesAuthor(resolver, email) {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
			matched = append(matched, id)
			if first.IsZero() || sp.first.Before(first) {
				first = sp.first
			}
			if sp.last.After(last) {
				last = sp.last
			}
		}
		if len(matched) == 0 {
			return s, matched, method, camperrors.Newf("no commits for author %q in %s", selector, p.Target)
		}
		sort.Strings(matched)
		estimated = 0
		share := 0.0
		for _, a := range r.all {
			for _, id := range matched {
				if a.ID == id {
					estimated += a.EstimatedPM
					share += a.Share
				}
			}
		}
		s.EstimatedPeople *= share
		s.EstimatedCost *= share
		s.ActualPeople = 1
		if !p.Shallow && !first.IsZero() {
			s.ActualPM = number(math.Max(minAuthorMonths, ElapsedMonths(first, last)))
			s.ElapsedMonths = s.ActualPM
		}
	} else if people > 0 {
		method = "repository_team_span_v1"
		s.ActualPeople = float64(people)
		if p.teamPM != nil {
			s.ElapsedMonths = p.teamPM
			s.ActualPM = number(*p.teamPM * float64(people))
		}
	} else {
		pm := 0.0
		qualified := false
		for _, a := range r.all {
			if a.Share*100 >= minAuthorPercentage && a.ElapsedPM != nil {
				pm += *a.ElapsedPM * a.Share
				qualified = true
			}
		}
		if qualified && r.Status != "partial" {
			s.ActualPM = number(math.Max(minAuthorMonths, pm))
		}
		s.ElapsedMonths = p.teamPM
	}
	if s.ActualPM != nil && *s.ActualPM > 0 {
		s.FullLeverage = number(estimated / *s.ActualPM)
	}
	if s.ActualPeople > 0 {
		s.SimpleLeverage = number(s.EstimatedPeople / s.ActualPeople)
	}
	return s, matched, method, nil
}

func mergeSpan(a *Contributor, sp reportSpan, complete bool) {
	a.HistoryComplete = a.HistoryComplete && complete && !sp.first.IsZero()
	if !sp.first.IsZero() && (a.First == nil || sp.first.Before(*a.First)) {
		v := sp.first.UTC()
		a.First = &v
	}
	if !sp.last.IsZero() && (a.Last == nil || sp.last.After(*a.Last)) {
		v := sp.last.UTC()
		a.Last = &v
	}
	if a.Name == "" {
		a.Name = strings.TrimSpace(sp.name)
	}
}
