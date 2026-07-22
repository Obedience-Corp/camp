package leverage

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestExpandAuthorFilter_GroupEmailsNoRawSubstring(t *testing.T) {
	cfg := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"alice": {
				DisplayName: "Alice",
				Emails:      []string{"alice@example.com", "alice@work.com"},
			},
			"malice": {
				DisplayName: "Malice",
				Emails:      []string{"malice@example.com"},
			},
		},
	}
	resolver := NewAuthorResolver(cfg)

	// Substring "alice" must match only the alice group — not malice, and must
	// not retain raw filter "alice" as a git substring query.
	match := ExpandAuthorFilter(resolver, "alice")
	if !match.Configured {
		t.Fatal("expected configured identity match")
	}
	if !match.AuthorIDs["alice"] {
		t.Fatalf("expected alice ID: %+v", match.AuthorIDs)
	}
	if match.AuthorIDs["malice"] {
		t.Fatal("alice filter must not match malice identity")
	}
	for _, e := range match.Emails {
		if e == "alice" || strings.EqualFold(e, "alice") {
			t.Fatalf("raw filter must not be retained after configured match: %v", match.Emails)
		}
		if strings.Contains(strings.ToLower(e), "malice") {
			t.Fatalf("malice email must not be included: %v", match.Emails)
		}
	}
	// Full group emails present.
	want := map[string]bool{"alice@example.com": true, "alice@work.com": true}
	for _, e := range match.Emails {
		delete(want, strings.ToLower(e))
	}
	if len(want) != 0 {
		t.Fatalf("missing group emails, leftover want: %v; got %v", want, match.Emails)
	}
}

func TestExpandAuthorFilter_AdHocKeepsRawFilter(t *testing.T) {
	match := ExpandAuthorFilter(NewAuthorResolver(nil), "someone@unlisted.dev")
	if match.Configured {
		t.Fatal("ad-hoc filter should not be configured")
	}
	if len(match.Emails) != 1 || match.Emails[0] != "someone@unlisted.dev" {
		t.Fatalf("ad-hoc emails = %v", match.Emails)
	}
	if len(match.AuthorIDs) != 0 {
		t.Fatalf("ad-hoc filter must leave AuthorIDs empty (got %v) so partial filters use git substring path", match.AuthorIDs)
	}
}

func TestExpandAuthorFilter_PartialAdHocNoCanonicalID(t *testing.T) {
	// Documented partial form: alice@co must not invent AuthorIDs["alice@co"].
	cfg := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"bob": {DisplayName: "Bob", Emails: []string{"bob@example.com"}},
		},
	}
	match := ExpandAuthorFilter(NewAuthorResolver(cfg), "alice@co")
	if match.Configured {
		t.Fatal("partial unlisted filter should not be configured")
	}
	if len(match.AuthorIDs) != 0 {
		t.Fatalf("AuthorIDs must stay empty for ad-hoc partial filters, got %v", match.AuthorIDs)
	}
}

func TestExpandAuthorFilter_FullDisplayName(t *testing.T) {
	cfg := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"alice-smith": {
				DisplayName: "Alice Smith",
				Emails:      []string{"alice@example.com"},
			},
		},
	}
	match := ExpandAuthorFilter(NewAuthorResolver(cfg), "Alice Smith")
	if !match.Configured || !match.AuthorIDs["alice-smith"] {
		t.Fatalf("full display name should match: configured=%v ids=%v", match.Configured, match.AuthorIDs)
	}
}

func TestExpandAuthorFilter_ExcludedNotConfigured(t *testing.T) {
	cfg := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"bot": {
				DisplayName: "Bot",
				Emails:      []string{"bot@example.com"},
				Exclude:     true,
			},
		},
	}
	match := ExpandAuthorFilter(NewAuthorResolver(cfg), "bot@example.com")
	if match.AuthorIDs["bot"] {
		t.Fatal("excluded author should not be added as configured identity")
	}
}

func TestMatchesAuthor_ConfiguredIsIDExclusive(t *testing.T) {
	cfg := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"alice":  {DisplayName: "Alice", Emails: []string{"alice@example.com"}},
			"malice": {DisplayName: "Malice", Emails: []string{"malice@example.com"}},
		},
	}
	resolver := NewAuthorResolver(cfg)
	match := ExpandAuthorFilter(resolver, "alice")

	if !match.MatchesAuthor(resolver, "alice@example.com") {
		t.Error("alice email should match")
	}
	if match.MatchesAuthor(resolver, "malice@example.com") {
		t.Error("malice email must not match alice filter (no raw substring)")
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
			// Would match raw substring "alice" but must not with configured match.
			{Email: "malice@example.com", Lines: 50},
		},
	}
	got := AuthorOwnershipFraction(proj, match, resolver)
	// 75 / (75+25+50) = 0.5
	if math.Abs(got-0.5) > 0.001 {
		t.Errorf("ownership = %f, want 0.5 (malice excluded)", got)
	}

	empty := ResolvedProject{}
	if AuthorOwnershipFraction(empty, match, resolver) != 1.0 {
		t.Error("empty authors should default ownership to 1.0")
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
	if math.Abs(score.FullLeverage-12.5) > 0.01 {
		t.Errorf("FullLeverage = %v, want 12.5", score.FullLeverage)
	}
}

func TestIsNoAuthorCommits(t *testing.T) {
	if !isNoAuthorCommits(errors.New("no commits by alice@x in /tmp/r")) {
		t.Error("expected no-commits detection")
	}
	if !isNoAuthorCommits(errors.New(`no commits matching author filter "alice" in /tmp/r`)) {
		t.Error("expected filter no-commits detection")
	}
	if isNoAuthorCommits(errors.New("git log: exit status 128")) {
		t.Error("operational git error must not be treated as no-commits")
	}
	if isNoAuthorCommits(context.Canceled) {
		t.Error("cancellation must not be treated as no-commits")
	}
}

func TestAuthorDateRangeMatch_PropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	match := AuthorMatch{Filter: "alice@example.com", Emails: []string{"alice@example.com"}}
	_, _, err := AuthorDateRangeMatch(ctx, "/nonexistent", match)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestAuthorActualPersonMonths_PropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	match := ExpandAuthorFilter(NewAuthorResolver(nil), "alice@example.com")
	_, err := AuthorActualPersonMonths(ctx, []ResolvedProject{{GitDir: "/tmp/x"}}, match, NewAuthorResolver(nil))
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestMergedAuthorSpans_PersonMonths(t *testing.T) {
	spans := MergedAuthorSpans{
		"alice": {
			earliest: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			latest:   time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	pm := spans.PersonMonths("alice")
	if pm < 7.5 || pm > 9.0 {
		t.Errorf("PersonMonths = %f, want ~8", pm)
	}
	if spans.PersonMonths("missing") != minAuthorMonths {
		t.Errorf("missing author should floor at %v", minAuthorMonths)
	}
}

func TestComputeProjectScore_AuthorScalesEstimateNoGit(t *testing.T) {
	// Pure unit path: Authors pre-populated; AuthorDateRangeMatch will fail for
	// missing git dir and fall back to minAuthorMonths elapsed.
	ctx := context.Background()
	proj := ResolvedProject{
		Name:   "p",
		GitDir: "/nonexistent-git-dir-for-unit-test",
		SCCDir: "/nonexistent",
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

	if math.Abs(score.EstimatedPeople-2.0) > 0.01 {
		t.Errorf("EstimatedPeople = %v, want 2.0 (25%% of 8)", score.EstimatedPeople)
	}
	if math.Abs(score.EstimatedCost-20000) > 0.01 {
		t.Errorf("EstimatedCost = %v, want 20000", score.EstimatedCost)
	}
	// Elapsed falls back to min when git dir is missing / no commits.
	if score.ActualPersonMonths != minAuthorMonths {
		t.Errorf("ActualPersonMonths = %v, want min floor %v", score.ActualPersonMonths, minAuthorMonths)
	}
}
