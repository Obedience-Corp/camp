package crawl

// Summary accumulates the result of a crawl session.
//
// It is owned by the shared package because both dungeon and intent
// crawl produce structurally similar tallies. Domain code (intent
// crawl, intent commit helper) reads Summary.Moved/Paths to drive
// commit messages and batch commit scoping.
//
// Moved keys are domain-defined target identifiers (for intent
// crawl that is the destination intent.Status string). Paths
// mirrors the same keys and stores campaign-root-relative
// destination paths in the order they were applied.
type Summary struct {
	Kept    int
	Skipped int
	Moved   map[string]int
	Paths   map[string][]string
}

// NewSummary returns an initialized Summary with empty maps.
// Callers should always use this constructor so RecordMove can
// safely insert into the maps without a nil check.
func NewSummary() *Summary {
	return &Summary{
		Moved: map[string]int{},
		Paths: map[string][]string{},
	}
}

// RecordKeep records a keep decision.
func (s *Summary) RecordKeep() { s.Kept++ }

// RecordSkip records a skip decision.
func (s *Summary) RecordSkip() { s.Skipped++ }

// RecordMove records a move decision to target with the given
// campaign-root-relative destination path.
func (s *Summary) RecordMove(target, relPath string) {
	s.Moved[target]++
	if relPath != "" {
		s.Paths[target] = append(s.Paths[target], relPath)
	}
}

// HasMoves reports whether any moves were applied.
func (s *Summary) HasMoves() bool {
	for _, n := range s.Moved {
		if n > 0 {
			return true
		}
	}
	return false
}

// MovedTotal returns the total number of items moved across all
// destinations.
func (s *Summary) MovedTotal() int {
	total := 0
	for _, n := range s.Moved {
		total += n
	}
	return total
}

// Total returns the total number of decisions recorded
// (keep + skip + moves).
func (s *Summary) Total() int {
	return s.Kept + s.Skipped + s.MovedTotal()
}
