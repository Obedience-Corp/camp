package leverage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AuthorIdentity represents a single author identity group.
// Multiple emails belonging to the same person are grouped together.
type AuthorIdentity struct {
	// DisplayName is the canonical name shown in output.
	DisplayName string `json:"display_name"`

	// Emails lists all email addresses associated with this author.
	Emails []string `json:"emails"`

	// Exclude removes this author from effort calculations.
	// Use for bot accounts, test accounts, etc.
	Exclude bool `json:"exclude,omitempty"`
}

// AuthorConfig is the schema for .campaign/leverage/authors.json.
type AuthorConfig struct {
	// Authors maps author IDs to their identity groups.
	Authors map[string]AuthorIdentity `json:"authors"`
}

// AuthorResolver maps email addresses to canonical author IDs.
// Loaded once at startup and threaded through join operations.
type AuthorResolver struct {
	config    *AuthorConfig
	emailToID map[string]string // lowercase email → author ID
}

// DefaultAuthorsPath returns the path to authors.json within the campaign.
func DefaultAuthorsPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, ".campaign", "leverage", "authors.json")
}

// LoadAuthorConfig reads authors.json from path.
// Returns nil, nil if the file does not exist.
func LoadAuthorConfig(path string) (*AuthorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading authors config: %w", err)
	}

	var cfg AuthorConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing authors config: %w", err)
	}

	return &cfg, nil
}

// SaveAuthorConfig writes the config to path as indented JSON.
func SaveAuthorConfig(path string, cfg *AuthorConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating authors config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling authors config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing authors config: %w", err)
	}
	return nil
}

// NewAuthorResolver builds a resolver from an AuthorConfig.
// If cfg is nil, returns a resolver that falls back to email-based IDs.
func NewAuthorResolver(cfg *AuthorConfig) *AuthorResolver {
	r := &AuthorResolver{
		config:    cfg,
		emailToID: make(map[string]string),
	}

	if cfg == nil {
		return r
	}

	for id, identity := range cfg.Authors {
		for _, email := range identity.Emails {
			r.emailToID[strings.ToLower(strings.TrimSpace(email))] = id
		}
	}

	return r
}

// Resolve returns the canonical author ID for an email address.
// Falls back to the lowercase email itself for unknown addresses.
func (r *AuthorResolver) Resolve(email string) string {
	key := strings.ToLower(strings.TrimSpace(email))
	if id, ok := r.emailToID[key]; ok {
		return id
	}
	return key
}

// DisplayName returns the display name for an author ID.
// Falls back to the ID itself if no display name is configured.
func (r *AuthorResolver) DisplayName(authorID string) string {
	if r.config != nil {
		if identity, ok := r.config.Authors[authorID]; ok && identity.DisplayName != "" {
			return identity.DisplayName
		}
	}
	return authorID
}

// IsExcluded returns true if the email belongs to an excluded author.
// Falls back to the hardcoded bot patterns for unknown emails.
func (r *AuthorResolver) IsExcluded(email string) bool {
	key := strings.ToLower(strings.TrimSpace(email))
	if id, ok := r.emailToID[key]; ok {
		if r.config != nil {
			if identity, ok := r.config.Authors[id]; ok {
				return identity.Exclude
			}
		}
	}
	// Fallback to hardcoded bot patterns for unconfigured emails.
	return isBotEmail(email)
}

// shortlogIdentity is a name+email pair from git shortlog.
type shortlogIdentity struct {
	name  string
	email string
}

// AutoDetectAuthors discovers author identity groups from git shortlog
// across all provided git directories. Uses union-find to transitively
// merge identities sharing either a normalized name or email address.
func AutoDetectAuthors(ctx context.Context, gitDirs []string) (*AuthorConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Collect all identities across all git dirs.
	var allIDs []shortlogIdentity
	for _, gitDir := range gitDirs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		ids, err := shortlogIdentities(ctx, gitDir)
		if err != nil {
			continue
		}
		allIDs = append(allIDs, ids...)
	}

	if len(allIDs) == 0 {
		return &AuthorConfig{Authors: make(map[string]AuthorIdentity)}, nil
	}

	// Union-find: merge identities sharing either name or email.
	uf := newUnionFind(len(allIDs))
	nameFirst := make(map[string]int)
	emailFirst := make(map[string]int)

	for i, id := range allIDs {
		normName := normalizeName(id.name)
		normEmail := strings.ToLower(strings.TrimSpace(id.email))

		if normName != "" {
			if prev, ok := nameFirst[normName]; ok {
				uf.union(i, prev)
			} else {
				nameFirst[normName] = i
			}
		}
		if normEmail != "" {
			if prev, ok := emailFirst[normEmail]; ok {
				uf.union(i, prev)
			} else {
				emailFirst[normEmail] = i
			}
		}
	}

	// Group identities by root.
	groups := make(map[int][]shortlogIdentity)
	for i, id := range allIDs {
		root := uf.find(i)
		groups[root] = append(groups[root], id)
	}

	// Build AuthorConfig from groups.
	cfg := &AuthorConfig{Authors: make(map[string]AuthorIdentity, len(groups))}
	usedIDs := make(map[string]bool)

	for _, members := range groups {
		// Collect unique emails and pick best display name.
		emailSet := make(map[string]bool)
		var bestName string
		for _, m := range members {
			normEmail := strings.ToLower(strings.TrimSpace(m.email))
			if normEmail != "" {
				emailSet[normEmail] = true
			}
			// Prefer longer, more descriptive names (likely real names vs usernames).
			if len(m.name) > len(bestName) {
				bestName = m.name
			}
		}

		emails := make([]string, 0, len(emailSet))
		for email := range emailSet {
			emails = append(emails, email)
		}
		if len(emails) == 0 {
			continue
		}

		// Generate a unique author ID from the display name.
		authorID := generateAuthorID(bestName, usedIDs)
		usedIDs[authorID] = true

		identity := AuthorIdentity{
			DisplayName: bestName,
			Emails:      emails,
		}

		// Auto-exclude detected bots.
		allBot := true
		for _, email := range emails {
			if !isBotEmail(email) {
				allBot = false
				break
			}
		}
		if allBot {
			identity.Exclude = true
		}

		cfg.Authors[authorID] = identity
	}

	return cfg, nil
}

// SyncAuthors merges newly discovered emails into an existing AuthorConfig.
// Emails already present in any group are skipped. New emails get their own
// single-email author entries.
func SyncAuthors(existing *AuthorConfig, discovered *AuthorConfig) bool {
	if existing == nil || discovered == nil {
		return false
	}

	// Build set of all known emails.
	known := make(map[string]bool)
	for _, identity := range existing.Authors {
		for _, email := range identity.Emails {
			known[strings.ToLower(strings.TrimSpace(email))] = true
		}
	}

	changed := false
	usedIDs := make(map[string]bool)
	for id := range existing.Authors {
		usedIDs[id] = true
	}

	for _, identity := range discovered.Authors {
		var newEmails []string
		for _, email := range identity.Emails {
			normEmail := strings.ToLower(strings.TrimSpace(email))
			if !known[normEmail] {
				newEmails = append(newEmails, normEmail)
				known[normEmail] = true
			}
		}

		if len(newEmails) == 0 {
			continue
		}

		// Add new emails as a new author entry.
		authorID := generateAuthorID(identity.DisplayName, usedIDs)
		usedIDs[authorID] = true

		existing.Authors[authorID] = AuthorIdentity{
			DisplayName: identity.DisplayName,
			Emails:      newEmails,
			Exclude:     identity.Exclude,
		}
		changed = true
	}

	return changed
}

// shortlogIdentities parses git shortlog output for a single repo.
func shortlogIdentities(ctx context.Context, gitDir string) ([]shortlogIdentity, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "shortlog", "-sne", "--all")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git shortlog: %w", err)
	}

	var ids []shortlogIdentity
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}

		name, email := parseNameEmail(strings.TrimSpace(parts[1]))
		if name == "" && email == "" {
			continue
		}

		ids = append(ids, shortlogIdentity{name: name, email: email})
	}

	return ids, nil
}

// generateAuthorID creates a slug-style ID from a display name.
// Ensures uniqueness against the usedIDs set by appending a suffix.
func generateAuthorID(name string, usedIDs map[string]bool) string {
	// Convert to lowercase slug.
	id := strings.ToLower(strings.TrimSpace(name))
	id = strings.ReplaceAll(id, " ", "-")

	// Remove non-alphanumeric characters except hyphens.
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	id = b.String()

	if id == "" {
		id = "unknown"
	}

	// Ensure uniqueness.
	base := id
	for i := 2; usedIDs[id]; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}

	return id
}
