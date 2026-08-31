package leverage

import (
	"strings"
	"testing"
	"time"

	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
)

func reportScore(name string) *intleverage.LeverageScore {
	return &intleverage.LeverageScore{
		ProjectName:        name,
		EstimatedPeople:    10,
		EstimatedMonths:    12,
		EstimatedCost:      1_204_993,
		ActualPeople:       1,
		ElapsedMonths:      3,
		ActualPersonMonths: 3.1,
		AuthorCount:        2,
		FullLeverage:       38.7,
		SimpleLeverage:     10,
		TotalFiles:         420,
		TotalCode:          58_231,
	}
}

func TestRenderScoreReport_IsPlainTextAndCarriesTheNumbers(t *testing.T) {
	agg := reportScore("")
	agg.AuthorCount = 3
	scores := []*intleverage.LeverageScore{reportScore("camp"), reportScore("fest")}
	cfg := &intleverage.LeverageConfig{
		ProjectStart: time.Date(2025, 4, 28, 0, 0, 0, 0, time.UTC),
	}

	report := renderScoreReport(agg, scores, cfg, leverageOutputOpts{})

	if strings.ContainsRune(report, '\x1b') {
		t.Fatalf("report must not carry terminal escapes:\n%q", report)
	}
	for _, want := range []string{
		"Camp Leverage: 38.7x (3 authors detected)",
		"PROJECT",
		"camp",
		"fest",
		"COCOMO Estimate: 120 person-months ($1,204,993)",
		"Actual Effort: 3.1 person-months",
		"Team Equivalent: 10.0x",
		"58,231 lines of code across 2 projects",
		"Since Apr 28, 2025",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q\nGot:\n%s", want, report)
		}
	}
}

func TestRenderScoreReport_PersonalAndExcludedVariants(t *testing.T) {
	cfg := &intleverage.LeverageConfig{ProjectStart: time.Date(2025, 4, 28, 0, 0, 0, 0, time.UTC)}
	scores := []*intleverage.LeverageScore{reportScore("camp")}

	tests := []struct {
		name string
		opts leverageOutputOpts
		want []string
	}{
		{
			name: "author filter switches the headline and effort label",
			opts: leverageOutputOpts{authorFilter: "lance@example.com"},
			want: []string{"Your Leverage: 38.7x (lance@example.com)", "Your Effort: 3.1 person-months"},
		},
		{
			name: "excluded projects are counted in the summary",
			opts: leverageOutputOpts{authorFilter: "lance@example.com", authorExcluded: 4},
			want: []string{"(4 excluded, no commits)"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := renderScoreReport(reportScore(""), scores, cfg, tc.opts)
			for _, want := range tc.want {
				if !strings.Contains(report, want) {
					t.Errorf("report missing %q\nGot:\n%s", want, report)
				}
			}
		})
	}
}

func TestRenderPlainTable(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		rows    [][]string
		want    string
	}{
		{
			name:    "no rows renders nothing",
			headers: []string{"A", "B"},
			rows:    nil,
			want:    "",
		},
		{
			name:    "columns are padded to the widest cell",
			headers: []string{"PROJECT", "CODE"},
			rows:    [][]string{{"camp", "58,231"}, {"obey-platform", "9"}},
			want: "PROJECT        CODE\n" +
				"camp           58,231\n" +
				"obey-platform  9\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderPlainTable(tc.headers, tc.rows); got != tc.want {
				t.Errorf("renderPlainTable() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestRenderSnapshotReport(t *testing.T) {
	tests := []struct {
		name   string
		count  int
		scores []*intleverage.LeverageScore
		want   []string
	}{
		{
			name:  "no snapshots renders the header alone",
			count: 0,
			want:  []string{"Saved 0 snapshots to .campaign/leverage/snapshots/"},
		},
		{
			name:   "one snapshot is singular and carries the table",
			count:  1,
			scores: []*intleverage.LeverageScore{reportScore("camp")},
			want:   []string{"Saved 1 snapshot to", "PROJECT", "camp"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := renderSnapshotReport(tc.count, tc.scores)
			for _, want := range tc.want {
				if !strings.Contains(report, want) {
					t.Errorf("report missing %q\nGot:\n%s", want, report)
				}
			}
		})
	}
}

func TestSubjectsNameTheFilteredProject(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		want    string
	}{
		{name: "snapshot all", subject: snapshotSubject(""), want: "snapshot"},
		{name: "snapshot one", subject: snapshotSubject("camp"), want: "snapshot camp"},
		{name: "backfill all", subject: backfillSubject(""), want: "backfill"},
		{name: "backfill one", subject: backfillSubject("camp"), want: "backfill camp"},
		{name: "reset all", subject: resetSubject(""), want: "reset"},
		{name: "reset one", subject: resetSubject("camp"), want: "reset camp"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.subject != tc.want {
				t.Errorf("subject = %q, want %q", tc.subject, tc.want)
			}
		})
	}
}

func TestRenderConfigReport(t *testing.T) {
	tests := []struct {
		name    string
		applied []string
		want    string
	}{
		{
			name:    "no recorded changes still describes the write",
			applied: nil,
			want:    "Updated leverage configuration.",
		},
		{
			name:    "applied changes are listed",
			applied: []string{"Team size: 3", "Autocommit: false"},
			want:    "Updated leverage configuration:\n\nTeam size: 3\nAutocommit: false",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderConfigReport(tc.applied); got != tc.want {
				t.Errorf("renderConfigReport() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderResetReport(t *testing.T) {
	tests := []struct {
		name    string
		project string
		cleared bool
		want    string
	}{
		{
			name:    "nothing cleared says so",
			cleared: false,
			want:    "No cached leverage data to clear for all projects.",
		},
		{
			name:    "cleared everything names the scope and the follow-up",
			cleared: true,
			want:    "Cleared cached leverage snapshots and blame data for all projects.",
		},
		{
			name:    "cleared one project names it",
			project: "camp",
			cleared: true,
			want:    "Cleared cached leverage snapshots and blame data for project camp.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := renderResetReport(tc.project, tc.cleared)
			if !strings.Contains(report, tc.want) {
				t.Errorf("report = %q, want it to contain %q", report, tc.want)
			}
			if tc.cleared && !strings.Contains(report, "camp leverage backfill") {
				t.Errorf("report should point at backfill:\n%s", report)
			}
		})
	}
}
