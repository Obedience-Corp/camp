package index

import (
	"math"
	"sort"
)

// SimilarResult represents an intent with its similarity score.
type SimilarResult struct {
	ID    string
	Score float64
}

// TermFrequency calculates TF (term frequency) for a term in a document.
// TF = count(term) / total_terms
func TermFrequency(termCount, totalTerms int) float64 {
	if totalTerms == 0 {
		return 0
	}
	return float64(termCount) / float64(totalTerms)
}

// InverseDocumentFrequency calculates IDF for a term across documents.
// IDF = log(total_docs / docs_containing_term)
func InverseDocumentFrequency(totalDocs, docsWithTerm int) float64 {
	if docsWithTerm == 0 {
		return 0
	}
	return math.Log(float64(totalDocs) / float64(docsWithTerm))
}

// TFIDF calculates the TF-IDF score for a term in a document.
func TFIDF(termCount, totalTerms, totalDocs, docsWithTerm int) float64 {
	tf := TermFrequency(termCount, totalTerms)
	idf := InverseDocumentFrequency(totalDocs, docsWithTerm)
	return tf * idf
}

// TFIDFVector represents a document as a vector of TF-IDF scores.
type TFIDFVector map[string]float64

// BuildTFIDFVector creates a TF-IDF vector for a document given corpus statistics.
func BuildTFIDFVector(wordFreq map[string]int, totalDocs int, docFreq map[string]int) TFIDFVector {
	vector := make(TFIDFVector)

	// Calculate total terms in document
	totalTerms := 0
	for _, count := range wordFreq {
		totalTerms += count
	}

	// Calculate TF-IDF for each term
	for term, count := range wordFreq {
		docsWithTerm := docFreq[term]
		if docsWithTerm == 0 {
			docsWithTerm = 1 // Avoid division by zero
		}
		vector[term] = TFIDF(count, totalTerms, totalDocs, docsWithTerm)
	}

	return vector
}

// CosineSimilarity calculates the cosine similarity between two TF-IDF vectors.
// Returns a value between 0 (no similarity) and 1 (identical).
func CosineSimilarity(a, b TFIDFVector) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	// Calculate dot product
	var dotProduct float64
	for term, scoreA := range a {
		if scoreB, ok := b[term]; ok {
			dotProduct += scoreA * scoreB
		}
	}

	// Calculate magnitudes
	var magA, magB float64
	for _, score := range a {
		magA += score * score
	}
	for _, score := range b {
		magB += score * score
	}

	magA = math.Sqrt(magA)
	magB = math.Sqrt(magB)

	if magA == 0 || magB == 0 {
		return 0
	}

	return dotProduct / (magA * magB)
}

// FindSimilar finds documents similar to a reference document.
// Returns results sorted by similarity score descending.
func FindSimilar(
	refVector TFIDFVector,
	vectors map[string]TFIDFVector,
	refID string,
	minScore float64,
) []SimilarResult {
	var results []SimilarResult

	for id, vector := range vectors {
		// Skip the reference document itself
		if id == refID {
			continue
		}

		score := CosineSimilarity(refVector, vector)
		if score >= minScore {
			results = append(results, SimilarResult{
				ID:    id,
				Score: score,
			})
		}
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// JaccardSimilarity calculates Jaccard similarity between two word sets.
// This is a simpler alternative to TF-IDF for quick similarity checks.
// Returns a value between 0 (no overlap) and 1 (identical sets).
func JaccardSimilarity(wordsA, wordsB []string) float64 {
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0
	}

	setA := make(map[string]bool)
	for _, w := range wordsA {
		setA[w] = true
	}

	setB := make(map[string]bool)
	for _, w := range wordsB {
		setB[w] = true
	}

	// Calculate intersection
	var intersection int
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}

	// Calculate union
	union := len(setA) + len(setB) - intersection

	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}
