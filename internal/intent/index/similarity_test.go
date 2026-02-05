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
		{"term in one doc", 10, 1, 2.302585},   // log(10)
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

func TestTagSimilarity(t *testing.T) {
	tests := []struct {
		name       string
		tagsA      []string
		tagsB      []string
		wantApprox float64
	}{
		{
			name:       "identical tags",
			tagsA:      []string{"api", "auth"},
			tagsB:      []string{"api", "auth"},
			wantApprox: 1.0,
		},
		{
			name:       "no overlap",
			tagsA:      []string{"api", "auth"},
			tagsB:      []string{"ui", "frontend"},
			wantApprox: 0.0,
		},
		{
			name:       "partial overlap",
			tagsA:      []string{"api", "auth"},
			tagsB:      []string{"api", "backend"},
			wantApprox: 0.333333, // 1/3
		},
		{
			name:       "empty first",
			tagsA:      []string{},
			tagsB:      []string{"api"},
			wantApprox: 0.0,
		},
		{
			name:       "both empty",
			tagsA:      []string{},
			tagsB:      []string{},
			wantApprox: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TagSimilarity(tt.tagsA, tt.tagsB)
			if math.Abs(got-tt.wantApprox) > 0.0001 {
				t.Errorf("TagSimilarity() = %v, want ~%v", got, tt.wantApprox)
			}
		})
	}
}

func TestMetadataBoost(t *testing.T) {
	tests := []struct {
		name       string
		a          *IndexedIntent
		b          *IndexedIntent
		wantApprox float64
	}{
		{
			name: "all metadata matches",
			a: &IndexedIntent{
				Type:     "feature",
				Concept:  "projects/camp",
				Priority: "high",
				Horizon:  "now",
			},
			b: &IndexedIntent{
				Type:     "feature",
				Concept:  "projects/camp",
				Priority: "high",
				Horizon:  "now",
			},
			wantApprox: WeightConcept + WeightType + WeightPriority + WeightHorizon, // 0.25
		},
		{
			name: "only concept matches",
			a: &IndexedIntent{
				Type:     "feature",
				Concept:  "projects/camp",
				Priority: "high",
				Horizon:  "now",
			},
			b: &IndexedIntent{
				Type:     "bug",
				Concept:  "projects/camp",
				Priority: "low",
				Horizon:  "later",
			},
			wantApprox: WeightConcept, // 0.15
		},
		{
			name: "no metadata matches",
			a: &IndexedIntent{
				Type:     "feature",
				Concept:  "projects/camp",
				Priority: "high",
				Horizon:  "now",
			},
			b: &IndexedIntent{
				Type:     "bug",
				Concept:  "projects/fest",
				Priority: "low",
				Horizon:  "later",
			},
			wantApprox: 0.0,
		},
		{
			name: "empty metadata - no match",
			a: &IndexedIntent{
				Type:    "",
				Concept: "",
			},
			b: &IndexedIntent{
				Type:    "",
				Concept: "",
			},
			wantApprox: 0.0, // Empty strings don't count as matches
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MetadataBoost(tt.a, tt.b)
			if math.Abs(got-tt.wantApprox) > 0.0001 {
				t.Errorf("MetadataBoost() = %v, want ~%v", got, tt.wantApprox)
			}
		})
	}
}

func TestCompositeSimilarity(t *testing.T) {
	tests := []struct {
		name       string
		a          *IndexedIntent
		b          *IndexedIntent
		tfidfScore float64
		wantMin    float64
		wantMax    float64
	}{
		{
			name: "high tfidf, matching tags and concept",
			a: &IndexedIntent{
				Tags:    []string{"api", "auth"},
				Concept: "projects/camp",
				Type:    "feature",
			},
			b: &IndexedIntent{
				Tags:    []string{"api", "auth"},
				Concept: "projects/camp",
				Type:    "feature",
			},
			tfidfScore: 0.8,
			// 0.5*0.8 (tfidf) + 0.25*1.0 (tags) + 0.15 (concept) + 0.05 (type)
			wantMin: 0.80,
			wantMax: 0.90,
		},
		{
			name: "zero tfidf, but matching metadata",
			a: &IndexedIntent{
				Tags:    []string{"api"},
				Concept: "projects/camp",
			},
			b: &IndexedIntent{
				Tags:    []string{"api"},
				Concept: "projects/camp",
			},
			tfidfScore: 0.0,
			// 0.5*0 (tfidf) + 0.25*1.0 (tags) + 0.15 (concept)
			wantMin: 0.35,
			wantMax: 0.45,
		},
		{
			name: "high tfidf, no metadata matches",
			a: &IndexedIntent{
				Tags:    []string{"api"},
				Concept: "projects/camp",
			},
			b: &IndexedIntent{
				Tags:    []string{"ui"},
				Concept: "projects/fest",
			},
			tfidfScore: 0.9,
			// 0.5*0.9 (tfidf) + 0.25*0 (tags) + 0 (no matches)
			wantMin: 0.40,
			wantMax: 0.50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompositeSimilarity(tt.a, tt.b, tt.tfidfScore)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("CompositeSimilarity() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestFindSimilarWithMetadata(t *testing.T) {
	refIntent := &IndexedIntent{
		ID:      "ref-id",
		Tags:    []string{"api", "auth"},
		Concept: "projects/camp",
		Type:    "feature",
	}
	refVector := TFIDFVector{"auth": 0.5, "login": 0.5}

	intents := map[string]*IndexedIntent{
		"ref-id": refIntent,
		"similar-tags-concept": {
			ID:      "similar-tags-concept",
			Tags:    []string{"api", "auth"},
			Concept: "projects/camp",
			Type:    "bug",
		},
		"similar-text-only": {
			ID:      "similar-text-only",
			Tags:    []string{"ui"},
			Concept: "projects/fest",
			Type:    "feature",
		},
		"different": {
			ID:      "different",
			Tags:    []string{"frontend"},
			Concept: "projects/guild",
			Type:    "chore",
		},
	}

	vectors := map[string]TFIDFVector{
		"ref-id":               refVector,
		"similar-tags-concept": {"auth": 0.5, "login": 0.5, "security": 0.2},
		"similar-text-only":    {"auth": 0.5, "signup": 0.5},
		"different":            {"navigation": 1.0, "routing": 1.0},
	}

	results := FindSimilarWithMetadata(refVector, vectors, refIntent, intents, "ref-id", 0.15)

	// Should not include ref-id
	for _, r := range results {
		if r.ID == "ref-id" {
			t.Error("FindSimilarWithMetadata should exclude reference document")
		}
	}

	// similar-tags-concept should rank higher due to tag + concept match
	if len(results) < 2 {
		t.Fatalf("FindSimilarWithMetadata returned %d results, want at least 2", len(results))
	}

	// Results should be sorted by score descending
	if results[0].Score < results[1].Score {
		t.Error("FindSimilarWithMetadata results should be sorted by score descending")
	}

	// similar-tags-concept should have highest score
	if results[0].ID != "similar-tags-concept" {
		t.Errorf("Expected similar-tags-concept to rank first, got %s", results[0].ID)
	}
}

func TestCompositeWeightsSumToOne(t *testing.T) {
	// Verify weights sum to 1.0 for predictable scoring
	totalWeight := WeightTFIDF + WeightTags + WeightConcept + WeightType + WeightPriority + WeightHorizon
	if math.Abs(totalWeight-1.0) > 0.0001 {
		t.Errorf("Composite weights sum to %v, should sum to 1.0", totalWeight)
	}
}
