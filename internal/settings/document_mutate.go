package settings

import "context"

// Set replaces an existing authored document with validated, exact source bytes.
func (s Service) Set(ctx context.Context, ref, expected string, source []byte) (Snapshot, error) {
	rel, err := s.mutationRef(ref, expected)
	if err != nil {
		return Snapshot{}, err
	}
	if len(source) > s.documentLimit() {
		return Snapshot{}, s.mutationError(CodeOversized, rel, "document exceeds the size limit")
	}
	s, locked, err := s.lockDocuments(ctx, rel)
	if err != nil {
		return Snapshot{}, err
	}
	defer locked.close()
	current, entry, err := s.existingMutation(locked.root, rel, expected)
	if err != nil {
		return Snapshot{}, err
	}
	if !entry.Capabilities.SourceEdit {
		return Snapshot{}, s.mutationError(CodeInvalidPath, rel, "document cannot be edited")
	}
	if err := s.validateMutation(rel, source, current); err != nil {
		return Snapshot{}, err
	}
	return s.persistMutation(ctx, locked.root, rel, source)
}
