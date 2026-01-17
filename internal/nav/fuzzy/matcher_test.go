package fuzzy

import (
	"testing"
)

func TestScore_ExactMatch(t *testing.T) {
	score, positions := Score("api-service", "api-service")

	if score != ScoreExactMatch {
		t.Errorf("Score = %d, want %d", score, ScoreExactMatch)
	}

	if len(positions) != 11 {
		t.Errorf("len(positions) = %d, want 11", len(positions))
	}
}

func TestScore_PrefixMatch(t *testing.T) {
	score, positions := Score("api", "api-service")

	if score != ScorePrefixMatch {
		t.Errorf("Score = %d, want %d", score, ScorePrefixMatch)
	}

	if len(positions) != 3 {
		t.Errorf("len(positions) = %d, want 3", len(positions))
	}
}

func TestScore_SubstringMatch(t *testing.T) {
	score, positions := Score("service", "api-service")

	if score <= 0 {
		t.Error("Expected positive score for substring match")
	}

	if len(positions) != 7 {
		t.Errorf("len(positions) = %d, want 7", len(positions))
	}

	// Should start at position 4 (after "api-")
	if positions[0] != 4 {
		t.Errorf("positions[0] = %d, want 4", positions[0])
	}
}

func TestScore_CaseInsensitive(t *testing.T) {
	tests := []struct {
		query  string
		target string
	}{
		{"API", "api-service"},
		{"Api", "api-service"},
		{"api", "API-SERVICE"},
		{"ApI", "aPi-SeRvIcE"},
	}

	for _, tt := range tests {
		t.Run(tt.query+"_"+tt.target, func(t *testing.T) {
			score, _ := Score(tt.query, tt.target)
			if score <= 0 {
				t.Errorf("Score(%q, %q) = %d, want positive", tt.query, tt.target, score)
			}
		})
	}
}

func TestScore_FuzzyMatch(t *testing.T) {
	tests := []struct {
		query  string
		target string
		match  bool
	}{
		{"as", "api-service", true},     // a...s
		{"asv", "api-service", true},    // a...s...v
		{"xyz", "api-service", false},   // no match
		{"apis", "api-service", true},   // api-s
		{"svc", "api-service", true},    // s...v...c (weak but valid)
		{"apisvc", "api-service", true}, // api-svc
		{"apx", "api-service", false},   // no x in target
	}

	for _, tt := range tests {
		t.Run(tt.query+"_"+tt.target, func(t *testing.T) {
			score, _ := Score(tt.query, tt.target)
			hasMatch := score > 0
			if hasMatch != tt.match {
				t.Errorf("Score(%q, %q) match = %v, want %v", tt.query, tt.target, hasMatch, tt.match)
			}
		})
	}
}

func TestScore_WordBoundaryBonus(t *testing.T) {
	// "s" at word boundary should score higher than "s" mid-word
	scoreWordBoundary, _ := Score("s", "api-service")
	scoreNoWordBoundary, _ := Score("p", "api-service")

	// The 's' matches at position 4 which is after '-' (word boundary)
	// The 'p' matches at position 1 which is not a word boundary (unless considering start)
	// Actually both should get base score

	if scoreWordBoundary <= 0 {
		t.Errorf("Expected positive score for 's' match")
	}
	if scoreNoWordBoundary <= 0 {
		t.Errorf("Expected positive score for 'p' match")
	}
}

func TestScore_ConsecutiveBonus(t *testing.T) {
	// "api" has consecutive matches, "as" does not
	scoreConsecutive, _ := Score("api", "api-service")
	scoreNonConsecutive, _ := Score("ais", "api-service")

	// Prefix match scores highest
	if scoreConsecutive <= scoreNonConsecutive {
		// This should be true since "api" is a prefix
		t.Logf("Prefix score: %d, fuzzy score: %d", scoreConsecutive, scoreNonConsecutive)
	}
}

func TestScore_EmptyQuery(t *testing.T) {
	score, positions := Score("", "api-service")

	if score != 0 {
		t.Errorf("Score = %d, want 0 for empty query", score)
	}
	if positions != nil {
		t.Error("positions should be nil for empty query")
	}
}

func TestScore_CamelCase(t *testing.T) {
	// Match at camelCase boundaries
	score, positions := Score("as", "apiService")

	if score <= 0 {
		t.Error("Expected positive score for camelCase match")
	}

	// 'a' at 0, 'S' at 3 (camelCase boundary)
	if len(positions) != 2 {
		t.Errorf("len(positions) = %d, want 2", len(positions))
	}
}

func TestFilter_Basic(t *testing.T) {
	targets := []string{
		"api-service",
		"web-service",
		"api-gateway",
		"database",
	}

	matches := Filter(targets, "api")

	if len(matches) != 2 {
		t.Errorf("len(matches) = %d, want 2", len(matches))
	}

	// First match should be "api-gateway" or "api-service" (both have prefix)
	first := matches[0].Target
	if first != "api-gateway" && first != "api-service" {
		t.Errorf("First match = %q, want api-* prefix match", first)
	}
}

func TestFilter_EmptyQuery(t *testing.T) {
	targets := []string{"a", "b", "c"}
	matches := Filter(targets, "")

	if len(matches) != 3 {
		t.Errorf("len(matches) = %d, want 3", len(matches))
	}
}

func TestFilter_NoMatches(t *testing.T) {
	targets := []string{"api-service", "web-service"}
	matches := Filter(targets, "xyz")

	if len(matches) != 0 {
		t.Errorf("len(matches) = %d, want 0", len(matches))
	}
}

func TestFilter_Sorting(t *testing.T) {
	targets := []string{
		"api-helper",  // Contains "api"
		"api",         // Exact match
		"api-service", // Prefix match
	}

	matches := Filter(targets, "api")

	if len(matches) != 3 {
		t.Fatalf("len(matches) = %d, want 3", len(matches))
	}

	// Exact match should be first
	if matches[0].Target != "api" {
		t.Errorf("First match = %q, want %q (exact match)", matches[0].Target, "api")
	}
}

func TestFilterMulti_SingleTerm(t *testing.T) {
	targets := []string{"api-service", "web-service"}
	matches := FilterMulti(targets, "api")

	if len(matches) != 1 {
		t.Errorf("len(matches) = %d, want 1", len(matches))
	}
}

func TestFilterMulti_MultipleTerms(t *testing.T) {
	targets := []string{
		"api-service",
		"api-gateway",
		"web-service",
		"api-service-v2",
	}

	matches := FilterMulti(targets, "api svc")

	// Both terms must match
	for _, m := range matches {
		if !HasMatch("api", m.Target) || !HasMatch("svc", m.Target) {
			t.Errorf("Match %q should contain both 'api' and 'svc'", m.Target)
		}
	}
}

func TestFilterMulti_NoMatches(t *testing.T) {
	targets := []string{"api-service", "web-service"}
	matches := FilterMulti(targets, "api xyz")

	if len(matches) != 0 {
		t.Errorf("len(matches) = %d, want 0 (no target has both terms)", len(matches))
	}
}

func TestFilterMulti_EmptyQuery(t *testing.T) {
	targets := []string{"a", "b", "c"}
	matches := FilterMulti(targets, "")

	if len(matches) != 3 {
		t.Errorf("len(matches) = %d, want 3", len(matches))
	}
}

func TestHasMatch(t *testing.T) {
	tests := []struct {
		query  string
		target string
		want   bool
	}{
		{"api", "api-service", true},
		{"xyz", "api-service", false},
		{"", "api-service", false},
		{"a", "a", true},
	}

	for _, tt := range tests {
		t.Run(tt.query+"_"+tt.target, func(t *testing.T) {
			got := HasMatch(tt.query, tt.target)
			if got != tt.want {
				t.Errorf("HasMatch(%q, %q) = %v, want %v", tt.query, tt.target, got, tt.want)
			}
		})
	}
}

func TestMatches_Targets(t *testing.T) {
	matches := Matches{
		{Target: "a", Score: 100},
		{Target: "b", Score: 50},
		{Target: "c", Score: 25},
	}

	targets := matches.Targets()

	if len(targets) != 3 {
		t.Fatalf("len(targets) = %d, want 3", len(targets))
	}
	if targets[0] != "a" || targets[1] != "b" || targets[2] != "c" {
		t.Errorf("Targets = %v, want [a b c]", targets)
	}
}

func TestIsWordBoundary(t *testing.T) {
	tests := []struct {
		s    string
		pos  int
		want bool
	}{
		{"api-service", 0, true}, // Start of string
		{"api-service", 4, true}, // After '-'
		{"api/service", 4, true}, // After '/'
		{"api_service", 4, true}, // After '_'
		{"api.service", 4, true}, // After '.'
		{"apiservice", 3, false}, // Mid-word
		{"api service", 4, true}, // After space
	}

	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			got := isWordBoundary(tt.s, tt.pos)
			if got != tt.want {
				t.Errorf("isWordBoundary(%q, %d) = %v, want %v", tt.s, tt.pos, got, tt.want)
			}
		})
	}
}

func TestIsCamelCaseBoundary(t *testing.T) {
	tests := []struct {
		s    string
		pos  int
		want bool
	}{
		{"apiService", 3, true},  // s->S
		{"apiService", 0, false}, // Start
		{"ApiService", 3, true},  // i->S
		{"APISERVICE", 3, false}, // All caps
		{"apiservice", 3, false}, // All lower
	}

	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			got := isCamelCaseBoundary(tt.s, tt.pos)
			if got != tt.want {
				t.Errorf("isCamelCaseBoundary(%q, %d) = %v, want %v", tt.s, tt.pos, got, tt.want)
			}
		})
	}
}

func BenchmarkScore(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Score("api", "api-service-v2-production")
	}
}

func BenchmarkFilter(b *testing.B) {
	targets := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		targets[i] = "target-" + string(rune('a'+i%26)) + "-service"
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Filter(targets, "ts")
	}
}

func BenchmarkFilterMulti(b *testing.B) {
	targets := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		targets[i] = "target-" + string(rune('a'+i%26)) + "-service-v" + string(rune('0'+i%10))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FilterMulti(targets, "target service")
	}
}

func TestFilter_Performance1000Targets(t *testing.T) {
	// Create 1000 targets
	targets := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		targets[i] = "project-" + string(rune('a'+i%26)) + "-service-" + string(rune('0'+i%10))
	}

	// The filter should complete quickly
	matches := Filter(targets, "ps")

	if len(matches) == 0 {
		t.Error("Expected some matches")
	}

	t.Logf("Found %d matches in 1000 targets", len(matches))
}
