package index

import (
	"math"
	"testing"
)

func TestTermFrequency(t *testing.T) {
	tests := []struct {
		name       string
		termCount  int
		totalTerms int
		want       float64
	}{
		{"single term", 1, 10, 0.1},
		{"multiple occurrences", 5, 10, 0.5},
		{"all same term", 10, 10, 1.0},
		{"zero total", 5, 0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TermFrequency(tt.termCount, tt.totalTerms)
			if got != tt.want {
				t.Errorf("TermFrequency() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInverseDocumentFrequency(t *testing.T) {
	tests := []struct {
		name         string
		totalDocs    int
		docsWithTerm int
		wantApprox   float64
	}{
		{"term in all docs", 10, 10, 0.0},
		{"term in one doc", 10, 1, 2.302585}, // log(10)
		{"term in half docs", 10, 5, 0.693147}, // log(2)
		{"zero docs with term", 10, 0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InverseDocumentFrequency(tt.totalDocs, tt.docsWithTerm)
			if math.Abs(got-tt.wantApprox) > 0.0001 {
				t.Errorf("InverseDocumentFrequency() = %v, want ~%v", got, tt.wantApprox)
			}
		})
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name       string
		a          TFIDFVector
		b          TFIDFVector
		wantApprox float64
	}{
		{
			name:       "identical vectors",
			a:          TFIDFVector{"auth": 0.5, "login": 0.5},
			b:          TFIDFVector{"auth": 0.5, "login": 0.5},
			wantApprox: 1.0,
		},
		{
			name:       "orthogonal vectors",
			a:          TFIDFVector{"auth": 1.0},
			b:          TFIDFVector{"navigation": 1.0},
			wantApprox: 0.0,
		},
		{
			name:       "partial overlap",
			a:          TFIDFVector{"auth": 1.0, "login": 1.0},
			b:          TFIDFVector{"auth": 1.0, "signup": 1.0},
			wantApprox: 0.5, // cos(60°)
		},
		{
			name:       "empty vector a",
			a:          TFIDFVector{},
			b:          TFIDFVector{"auth": 1.0},
			wantApprox: 0.0,
		},
		{
			name:       "empty vector b",
			a:          TFIDFVector{"auth": 1.0},
			b:          TFIDFVector{},
			wantApprox: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.wantApprox) > 0.0001 {
				t.Errorf("CosineSimilarity() = %v, want ~%v", got, tt.wantApprox)
			}
		})
	}
}

func TestFindSimilar(t *testing.T) {
	refVector := TFIDFVector{"auth": 0.5, "login": 0.5}
	vectors := map[string]TFIDFVector{
		"ref-id":    refVector, // Should be excluded
		"similar":   {"auth": 0.5, "login": 0.5, "security": 0.2},
		"partial":   {"auth": 0.5, "signup": 0.5},
		"different": {"navigation": 1.0, "routing": 1.0},
	}

	results := FindSimilar(refVector, vectors, "ref-id", 0.3)

	// Should not include ref-id
	for _, r := range results {
		if r.ID == "ref-id" {
			t.Error("FindSimilar should exclude reference document")
		}
	}

	// Should include similar and partial, not different
	if len(results) != 2 {
		t.Errorf("FindSimilar returned %d results, want 2", len(results))
	}

	// Results should be sorted by score descending
	if len(results) >= 2 && results[0].Score < results[1].Score {
		t.Error("FindSimilar results should be sorted by score descending")
	}
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		name       string
		wordsA     []string
		wordsB     []string
		wantApprox float64
	}{
		{
			name:       "identical sets",
			wordsA:     []string{"auth", "login"},
			wordsB:     []string{"auth", "login"},
			wantApprox: 1.0,
		},
		{
			name:       "no overlap",
			wordsA:     []string{"auth", "login"},
			wordsB:     []string{"navigation", "routing"},
			wantApprox: 0.0,
		},
		{
			name:       "partial overlap",
			wordsA:     []string{"auth", "login"},
			wordsB:     []string{"auth", "signup"},
			wantApprox: 0.333333, // 1/3
		},
		{
			name:       "subset",
			wordsA:     []string{"auth"},
			wordsB:     []string{"auth", "login", "signup"},
			wantApprox: 0.333333, // 1/3
		},
		{
			name:       "empty first",
			wordsA:     []string{},
			wordsB:     []string{"auth"},
			wantApprox: 0.0,
		},
		{
			name:       "empty second",
			wordsA:     []string{"auth"},
			wordsB:     []string{},
			wantApprox: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JaccardSimilarity(tt.wordsA, tt.wordsB)
			if math.Abs(got-tt.wantApprox) > 0.0001 {
				t.Errorf("JaccardSimilarity() = %v, want ~%v", got, tt.wantApprox)
			}
		})
	}
}

func TestBuildTFIDFVector(t *testing.T) {
	wordFreq := map[string]int{
		"auth":  3,
		"login": 1,
	}
	docFreq := map[string]int{
		"auth":  2, // appears in 2 of 10 docs
		"login": 5, // appears in 5 of 10 docs
	}
	totalDocs := 10

	vector := BuildTFIDFVector(wordFreq, totalDocs, docFreq)

	// auth: TF = 3/4 = 0.75, IDF = log(10/2) = 1.609
	// login: TF = 1/4 = 0.25, IDF = log(10/5) = 0.693
	if vector["auth"] == 0 {
		t.Error("auth should have non-zero TF-IDF")
	}
	if vector["login"] == 0 {
		t.Error("login should have non-zero TF-IDF")
	}

	// auth should have higher TF-IDF (higher TF, higher IDF)
	if vector["auth"] <= vector["login"] {
		t.Error("auth should have higher TF-IDF than login")
	}
}
