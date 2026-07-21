package leverage

import (
	"context"
	"math"
	"testing"
)

func TestExpandAuthorFilter_GroupEmails(t *testing.T) {
	cfg := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"lancekrogers": {
				DisplayName: "lancekrogers",
				Emails: []string{
					"lancekrogers@gmail.com",
					"lance@blockhead.consulting",
				},
			},
			"bot": {
				DisplayName: "Bot",
				Emails:      []string{"bot@example.com"},
				Exclude:     true,
			},
		},
	}
	resolver := NewAuthorResolver(cfg)

	match := ExpandAuthorFilter(resolver, "lancekrogers@gmail.com")
	if !match.AuthorIDs["lancekrogers"] {
		t.Fatalf("expected lancekrogers ID matched: %+v", match.AuthorIDs)
	}
	hasBlockhead := false
	for _, e := range match.Emails {
		if e == "lance@blockhead.consulting" {
			hasBlockhead = true
		}
	}
	if !hasBlockhead {
		t.Fatalf("expected alternate email in match: %v", match.Emails)
	}

	// Excluded identities must not match by email alone via expand of their id.
	botMatch := ExpandAuthorFilter(resolver, "bot@example.com")
	if botMatch.AuthorIDs["bot"] {
		t.Fatal("excluded author should not be added via group match")
	}
}

func TestExpandAuthorFilter_ByDisplayName(t *testing.T) {
	cfg := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"alice-smith": {
				DisplayName: "Alice Smith",
				Emails:      []string{"alice@work.com", "alice@personal.com"},
			},
		},
	}
	match := ExpandAuthorFilter(NewAuthorResolver(cfg), "Alice")
	if !match.AuthorIDs["alice-smith"] {
		t.Fatalf("display name substring should match: %+v", match.AuthorIDs)
	}
}

func TestAuthorOwnershipFraction(t *testing.T) {
	resolver := NewAuthorResolver(&AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"alice": {DisplayName: "Alice", Emails: []string{"alice@example.com"}},
		},
	})
	match := ExpandAuthorFilter(resolver, "alice@example.com")

	proj := ResolvedProject{
		Authors: []AuthorContribution{
			{Email: "alice@example.com", Lines: 75},
			{Email: "bob@example.com", Lines: 25},
		},
	}
	got := AuthorOwnershipFraction(proj, match, resolver)
	if math.Abs(got-0.75) > 0.001 {
		t.Errorf("ownership = %f, want 0.75", got)
	}

	// Missing blame → full ownership so committed projects are not zeroed.
	empty := ResolvedProject{}
	if AuthorOwnershipFraction(empty, match, resolver) != 1.0 {
		t.Error("empty authors should default ownership to 1.0")
	}
}

func TestAuthorActualPersonMonths_DeduplicatesAcrossRepos(t *testing.T) {
	ctx := context.Background()

	repo1 := initGitRepo(t)
	repo2 := initGitRepo(t)

	// Alice active Jan–Sep in repo1 (~8 months) and Mar–Jul in repo2 (overlap).
	commitFileWithDate(t, repo1, "a.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, repo1, "b.go", "package a\nvar x = 1\n", "Alice", "alice@example.com", "2025-09-01T12:00:00+00:00")
	commitFileWithDate(t, repo2, "c.go", "package b\n", "Alice", "alice@example.com", "2025-03-01T12:00:00+00:00")
	commitFileWithDate(t, repo2, "d.go", "package b\nvar y = 1\n", "Alice", "alice@example.com", "2025-07-01T12:00:00+00:00")

	projects := []ResolvedProject{
		{Name: "p1", GitDir: repo1, SCCDir: repo1},
		{Name: "p2", GitDir: repo2, SCCDir: repo2},
	}
	match := ExpandAuthorFilter(NewAuthorResolver(nil), "alice@example.com")

	pm, err := AuthorActualPersonMonths(ctx, projects, match)
	if err != nil {
		t.Fatalf("AuthorActualPersonMonths: %v", err)
	}
	// Union span is ~8 months, not 8+4.
	if pm < 7.5 || pm > 9.0 {
		t.Errorf("author PM = %.2f, want ~8.0 (union across repos)", pm)
	}

	// Naive sum of per-repo spans must be larger.
	first1, last1, err := AuthorDateRange(ctx, repo1, "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	first2, last2, err := AuthorDateRange(ctx, repo2, "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	naive := ElapsedMonths(first1, last1) + ElapsedMonths(first2, last2)
	if pm >= naive-0.01 {
		t.Errorf("union PM (%.2f) should be less than naive sum (%.2f)", pm, naive)
	}
}

func TestAuthorActualPersonMonths_DeduplicatesMonorepoGitDir(t *testing.T) {
	ctx := context.Background()
	repo := initGitRepo(t)
	commitFileWithDate(t, repo, "a.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, repo, "b.go", "package a\nvar x = 1\n", "Alice", "alice@example.com", "2025-07-01T12:00:00+00:00")

	projects := []ResolvedProject{
		{Name: "root", GitDir: repo, SCCDir: repo},
		{Name: "sub1", GitDir: repo, SCCDir: repo + "/sub1", InMonorepo: true},
		{Name: "sub2", GitDir: repo, SCCDir: repo + "/sub2", InMonorepo: true},
	}
	match := ExpandAuthorFilter(NewAuthorResolver(nil), "alice@example.com")
	pm, err := AuthorActualPersonMonths(ctx, projects, match)
	if err != nil {
		t.Fatalf("AuthorActualPersonMonths: %v", err)
	}
	// ~6 months once, not 3×.
	if pm < 5.5 || pm > 7.0 {
		t.Errorf("author PM = %.2f, want ~6.0 for monorepo shared GitDir", pm)
	}
}

func TestScaleScoreForAuthor(t *testing.T) {
	score := &LeverageScore{
		EstimatedPeople:    10,
		EstimatedMonths:    5,
		EstimatedCost:      1000,
		ActualPeople:       1,
		ActualPersonMonths: 2,
	}
	ScaleScoreForAuthor(score, 0.5)
	if score.EstimatedPeople != 5 {
		t.Errorf("EstimatedPeople = %v, want 5", score.EstimatedPeople)
	}
	if score.EstimatedCost != 500 {
		t.Errorf("EstimatedCost = %v, want 500", score.EstimatedCost)
	}
	// Full leverage recomputed: (5*5)/2 = 12.5
	if math.Abs(score.FullLeverage-12.5) > 0.01 {
		t.Errorf("FullLeverage = %v, want 12.5", score.FullLeverage)
	}
}

func TestComputeProjectScore_AuthorScalesEstimate(t *testing.T) {
	ctx := context.Background()
	repo := initGitRepo(t)
	commitFileWithDate(t, repo, "a.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, repo, "b.go", "package a\nvar x = 1\n", "Alice", "alice@example.com", "2025-07-01T12:00:00+00:00")
	// Bob owns more lines in blame if we only set Authors manually.

	proj := ResolvedProject{
		Name:   "p",
		GitDir: repo,
		SCCDir: repo,
		Authors: []AuthorContribution{
			{Email: "alice@example.com", Lines: 25, Percentage: 25},
			{Email: "bob@example.com", Lines: 75, Percentage: 75},
		},
	}
	result := &SCCResult{
		EstimatedPeople:         8,
		EstimatedScheduleMonths: 10,
		EstimatedCost:           80000,
		LanguageSummary:         []LanguageEntry{{Lines: 100, Code: 80}},
	}
	resolver := NewAuthorResolver(nil)
	match := ExpandAuthorFilter(resolver, "alice@example.com")

	score := ComputeProjectScore(ctx, proj, result, ProjectScoreParams{
		AuthorMatch: match,
		Resolver:    resolver,
	})

	// Ownership 25% → estimated people scaled.
	if math.Abs(score.EstimatedPeople-2.0) > 0.01 {
		t.Errorf("EstimatedPeople = %v, want 2.0 (25%% of 8)", score.EstimatedPeople)
	}
	if math.Abs(score.EstimatedCost-20000) > 0.01 {
		t.Errorf("EstimatedCost = %v, want 20000", score.EstimatedCost)
	}
	if score.ActualPeople != 1 {
		t.Errorf("ActualPeople = %v, want 1", score.ActualPeople)
	}
}

func TestPersonalAggregate_NoMultiCount(t *testing.T) {
	// Simulates main_command personal rewrite: ownership-scaled project est
	// summed, actual from AuthorActualPersonMonths (union).
	ctx := context.Background()
	repo1 := initGitRepo(t)
	repo2 := initGitRepo(t)

	commitFileWithDate(t, repo1, "a.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, repo1, "b.go", "package a\nvar x = 1\n", "Alice", "alice@example.com", "2025-09-01T12:00:00+00:00")
	commitFileWithDate(t, repo2, "c.go", "package b\n", "Alice", "alice@example.com", "2025-03-01T12:00:00+00:00")
	commitFileWithDate(t, repo2, "d.go", "package b\nvar y = 1\n", "Alice", "alice@example.com", "2025-07-01T12:00:00+00:00")

	resolver := NewAuthorResolver(nil)
	match := ExpandAuthorFilter(resolver, "alice@example.com")

	projects := []ResolvedProject{
		{
			Name: "p1", GitDir: repo1, SCCDir: repo1,
			Authors: []AuthorContribution{{Email: "alice@example.com", Lines: 100, Percentage: 100}},
		},
		{
			Name: "p2", GitDir: repo2, SCCDir: repo2,
			Authors: []AuthorContribution{{Email: "alice@example.com", Lines: 100, Percentage: 100}},
		},
	}
	results := []*SCCResult{
		{EstimatedPeople: 10, EstimatedScheduleMonths: 5, EstimatedCost: 1, LanguageSummary: []LanguageEntry{{Lines: 10, Code: 8}}},
		{EstimatedPeople: 10, EstimatedScheduleMonths: 5, EstimatedCost: 1, LanguageSummary: []LanguageEntry{{Lines: 10, Code: 8}}},
	}

	var scores []*LeverageScore
	for i, p := range projects {
		scores = append(scores, ComputeProjectScore(ctx, p, results[i], ProjectScoreParams{
			AuthorMatch: match,
			Resolver:    resolver,
		}))
	}

	// Broken path: sum of project actuals ~ 8+4 = 12
	var sumActual float64
	var sumEst float64
	for _, s := range scores {
		sumActual += s.ActualPersonMonths
		sumEst += s.EstimatedPeople * s.EstimatedMonths
	}

	unionActual, err := AuthorActualPersonMonths(ctx, projects, match)
	if err != nil {
		t.Fatal(err)
	}
	if unionActual >= sumActual-0.01 {
		t.Fatalf("union actual %.2f should be < sum actual %.2f", unionActual, sumActual)
	}

	brokenLev := sumEst / sumActual
	fixedLev := sumEst / unionActual
	if fixedLev <= brokenLev {
		t.Fatalf("fixed leverage %.2f should exceed broken %.2f", fixedLev, brokenLev)
	}
	// Est is 50+50=100, union actual ~8 → leverage ~12.5; broken ~100/12 ~ 8.3
	if fixedLev < 10 || fixedLev > 15 {
		t.Errorf("fixed leverage = %.2f, want ~12.5", fixedLev)
	}
}
