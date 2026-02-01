package index

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/obediencecorp/camp/internal/intent"
)

// IndexedIntent contains processed content for matching.
type IndexedIntent struct {
	ID         string
	Title      string
	Status     intent.Status
	Tags       []string       // From frontmatter
	Hashtags   []string       // Parsed from content
	Words      []string       // Tokenized words
	WordFreq   map[string]int // Word frequency
	TotalWords int

	// Metadata fields for composite similarity matching
	Type     intent.Type     // Intent type (idea, feature, bug, etc.)
	Concept  string          // Concept path (projects/camp, etc.)
	Priority intent.Priority // Priority level
	Horizon  intent.Horizon  // Time horizon
}

// Index provides fast content-based lookup for intents.
type Index struct {
	intentsDir string

	// Primary indexes
	tagIndex     map[string][]string // tag -> []intentID
	hashtagIndex map[string][]string // hashtag -> []intentID

	// Document frequency for TF-IDF
	docFreq map[string]int // word -> number of docs containing it

	// Cached intent data
	intents map[string]*IndexedIntent

	// TF-IDF vectors
	vectors map[string]TFIDFVector

	// Metadata
	buildTime time.Time

	mu sync.RWMutex
}

// NewIndex creates a new empty index.
func NewIndex(intentsDir string) *Index {
	return &Index{
		intentsDir:   intentsDir,
		tagIndex:     make(map[string][]string),
		hashtagIndex: make(map[string][]string),
		docFreq:      make(map[string]int),
		intents:      make(map[string]*IndexedIntent),
		vectors:      make(map[string]TFIDFVector),
	}
}

// DefaultStatuses returns the statuses to index by default.
// Only indexes active working set (inbox, active, ready).
func DefaultStatuses() []intent.Status {
	return []intent.Status{
		intent.StatusInbox,
		intent.StatusActive,
		intent.StatusReady,
	}
}

// Build scans intents and builds the index.
// By default, only indexes inbox/active/ready statuses.
func (idx *Index) Build(ctx context.Context, statuses []intent.Status) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Clear existing data
	idx.tagIndex = make(map[string][]string)
	idx.hashtagIndex = make(map[string][]string)
	idx.docFreq = make(map[string]int)
	idx.intents = make(map[string]*IndexedIntent)
	idx.vectors = make(map[string]TFIDFVector)

	if statuses == nil {
		statuses = DefaultStatuses()
	}

	// First pass: collect all intents and document frequencies
	for _, status := range statuses {
		statusDir := filepath.Join(idx.intentsDir, string(status))
		entries, err := os.ReadDir(statusDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}

		for _, entry := range entries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				continue
			}

			path := filepath.Join(statusDir, entry.Name())
			if err := idx.indexFile(path, status); err != nil {
				// Skip files that fail to parse
				continue
			}
		}
	}

	// Second pass: build TF-IDF vectors
	totalDocs := len(idx.intents)
	for id, indexed := range idx.intents {
		idx.vectors[id] = BuildTFIDFVector(indexed.WordFreq, totalDocs, idx.docFreq)
	}

	idx.buildTime = time.Now()
	return nil
}

// indexFile reads and indexes a single intent file.
func (idx *Index) indexFile(path string, status intent.Status) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	parsed, err := intent.ParseIntentFromFile(path, data)
	if err != nil {
		return err
	}

	// Combine title and content for word extraction
	fullContent := parsed.Title + "\n" + parsed.Content

	// Extract words and hashtags
	words := ExtractWords(fullContent)
	hashtags := ExtractHashtags(parsed.Content)
	wordFreq := WordFrequency(fullContent)

	// Create indexed intent
	indexed := &IndexedIntent{
		ID:         parsed.ID,
		Title:      parsed.Title,
		Status:     status,
		Tags:       parsed.Tags,
		Hashtags:   hashtags,
		Words:      words,
		WordFreq:   wordFreq,
		TotalWords: len(words),
		// Metadata fields for composite similarity
		Type:     parsed.Type,
		Concept:  parsed.Concept,
		Priority: parsed.Priority,
		Horizon:  parsed.Horizon,
	}

	idx.intents[parsed.ID] = indexed

	// Update tag index
	for _, tag := range parsed.Tags {
		idx.tagIndex[tag] = append(idx.tagIndex[tag], parsed.ID)
	}

	// Update hashtag index
	for _, hashtag := range hashtags {
		idx.hashtagIndex[hashtag] = append(idx.hashtagIndex[hashtag], parsed.ID)
	}

	// Update document frequency
	seen := make(map[string]bool)
	for _, word := range words {
		if !seen[word] {
			seen[word] = true
			idx.docFreq[word]++
		}
	}

	return nil
}

// FindByTag returns intent IDs matching a frontmatter tag.
func (idx *Index) FindByTag(tag string) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	ids := idx.tagIndex[tag]
	if len(ids) == 0 {
		return nil
	}

	// Return a copy to avoid mutation
	result := make([]string, len(ids))
	copy(result, ids)
	return result
}

// FindByHashtag returns intent IDs containing a hashtag in content.
func (idx *Index) FindByHashtag(hashtag string) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	ids := idx.hashtagIndex[hashtag]
	if len(ids) == 0 {
		return nil
	}

	result := make([]string, len(ids))
	copy(result, ids)
	return result
}

// FindSimilar returns intents similar to the given ID using composite similarity.
// The composite score combines TF-IDF text similarity with metadata matching
// (tags, concept, type, priority, horizon).
func (idx *Index) FindSimilar(id string, minScore float64) []SimilarResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	refVector, ok := idx.vectors[id]
	if !ok {
		return nil
	}

	refIntent := idx.intents[id]
	if refIntent == nil {
		return nil
	}

	return FindSimilarWithMetadata(refVector, idx.vectors, refIntent, idx.intents, id, minScore)
}

// GetAllTags returns all unique frontmatter tags.
func (idx *Index) GetAllTags() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	tags := make([]string, 0, len(idx.tagIndex))
	for tag := range idx.tagIndex {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

// GetAllHashtags returns all unique content hashtags.
func (idx *Index) GetAllHashtags() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	hashtags := make([]string, 0, len(idx.hashtagIndex))
	for hashtag := range idx.hashtagIndex {
		hashtags = append(hashtags, hashtag)
	}
	sort.Strings(hashtags)
	return hashtags
}

// GetIndexedIntent returns the indexed data for an intent.
func (idx *Index) GetIndexedIntent(id string) *IndexedIntent {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return idx.intents[id]
}

// Size returns the number of indexed intents.
func (idx *Index) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return len(idx.intents)
}

// BuildTime returns when the index was last built.
func (idx *Index) BuildTime() time.Time {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return idx.buildTime
}

// TagCounts returns tags with their occurrence counts.
func (idx *Index) TagCounts() map[string]int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	counts := make(map[string]int)
	for tag, ids := range idx.tagIndex {
		counts[tag] = len(ids)
	}
	return counts
}

// HashtagCounts returns hashtags with their occurrence counts.
func (idx *Index) HashtagCounts() map[string]int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	counts := make(map[string]int)
	for hashtag, ids := range idx.hashtagIndex {
		counts[hashtag] = len(ids)
	}
	return counts
}
