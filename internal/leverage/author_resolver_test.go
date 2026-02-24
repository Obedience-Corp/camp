package leverage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewAuthorResolver_Nil(t *testing.T) {
	r := NewAuthorResolver(nil)
	if r == nil {
		t.Fatal("expected non-nil resolver")
	}

	// Should fall back to email as ID.
	if got := r.Resolve("alice@example.com"); got != "alice@example.com" {
		t.Errorf("Resolve = %q, want email fallback", got)
	}
	if got := r.DisplayName("alice@example.com"); got != "alice@example.com" {
		t.Errorf("DisplayName = %q, want ID fallback", got)
	}
	if r.IsExcluded("alice@example.com") {
		t.Error("unexpected exclusion")
	}
}

func TestAuthorResolver_Resolve(t *testing.T) {
	cfg := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"lance": {
				DisplayName: "Lance Rogers",
				Emails:      []string{"lance@blockhead.consulting", "lancekrogers@gmail.com"},
			},
		},
	}
	r := NewAuthorResolver(cfg)

	tests := []struct {
		email string
		want  string
	}{
		{"lance@blockhead.consulting", "lance"},
		{"lancekrogers@gmail.com", "lance"},
		{"LANCE@blockhead.consulting", "lance"},        // case insensitive
		{" lancekrogers@gmail.com ", "lance"},          // trimmed
		{"unknown@example.com", "unknown@example.com"}, // fallback
	}

	for _, tt := range tests {
		if got := r.Resolve(tt.email); got != tt.want {
			t.Errorf("Resolve(%q) = %q, want %q", tt.email, got, tt.want)
		}
	}
}

func TestAuthorResolver_DisplayName(t *testing.T) {
	cfg := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"lance":   {DisplayName: "Lance Rogers", Emails: []string{"lance@example.com"}},
			"no-name": {Emails: []string{"anon@example.com"}},
		},
	}
	r := NewAuthorResolver(cfg)

	if got := r.DisplayName("lance"); got != "Lance Rogers" {
		t.Errorf("DisplayName(lance) = %q, want %q", got, "Lance Rogers")
	}
	if got := r.DisplayName("no-name"); got != "no-name" {
		t.Errorf("DisplayName(no-name) = %q, want ID fallback", got)
	}
	if got := r.DisplayName("unknown"); got != "unknown" {
		t.Errorf("DisplayName(unknown) = %q, want ID fallback", got)
	}
}

func TestAuthorResolver_IsExcluded(t *testing.T) {
	cfg := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"bot": {
				DisplayName: "Test Bot",
				Emails:      []string{"bot@ci.local"},
				Exclude:     true,
			},
			"human": {
				DisplayName: "Human",
				Emails:      []string{"human@example.com"},
			},
		},
	}
	r := NewAuthorResolver(cfg)

	if !r.IsExcluded("bot@ci.local") {
		t.Error("bot should be excluded")
	}
	if r.IsExcluded("human@example.com") {
		t.Error("human should not be excluded")
	}
	// Fallback to hardcoded patterns for unknown emails.
	if !r.IsExcluded("noreply@github.com") {
		t.Error("noreply should be excluded by fallback")
	}
	if r.IsExcluded("unknown@example.com") {
		t.Error("unknown non-bot should not be excluded")
	}
}

func TestAuthorConfig_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "authors.json")

	cfg := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"alice": {
				DisplayName: "Alice Smith",
				Emails:      []string{"alice@example.com", "alice@work.com"},
			},
			"bot": {
				DisplayName: "CI Bot",
				Emails:      []string{"bot@ci.local"},
				Exclude:     true,
			},
		},
	}

	if err := SaveAuthorConfig(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadAuthorConfig(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded nil")
	}

	if len(loaded.Authors) != 2 {
		t.Errorf("Authors count = %d, want 2", len(loaded.Authors))
	}
	alice := loaded.Authors["alice"]
	if alice.DisplayName != "Alice Smith" {
		t.Errorf("alice.DisplayName = %q", alice.DisplayName)
	}
	if len(alice.Emails) != 2 {
		t.Errorf("alice.Emails count = %d, want 2", len(alice.Emails))
	}
	if !loaded.Authors["bot"].Exclude {
		t.Error("bot should be excluded")
	}
}

func TestLoadAuthorConfig_Missing(t *testing.T) {
	cfg, err := LoadAuthorConfig(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Error("expected nil for missing file")
	}
}

func TestLoadAuthorConfig_Corrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadAuthorConfig(path)
	if err == nil {
		t.Error("expected error for corrupt JSON")
	}
}

func TestAutoDetectAuthors(t *testing.T) {
	dir := initGitRepo(t)

	// Create commits with different identities.
	commitFile(t, dir, "a.go", "package a\n", "Alice Smith", "alice@work.com")
	commitFile(t, dir, "b.go", "package b\n", "alice smith", "alice@home.com") // same name, different email
	commitFile(t, dir, "c.go", "package c\n", "Bob", "bob@example.com")

	ctx := context.Background()
	cfg, err := AutoDetectAuthors(ctx, []string{dir})
	if err != nil {
		t.Fatalf("AutoDetectAuthors: %v", err)
	}

	if len(cfg.Authors) != 2 {
		t.Fatalf("expected 2 author groups, got %d: %+v", len(cfg.Authors), cfg.Authors)
	}

	// Alice's two emails should be merged (same normalized name).
	var aliceGroup *AuthorIdentity
	for _, identity := range cfg.Authors {
		if len(identity.Emails) == 2 {
			aliceGroup = &identity
			break
		}
	}
	if aliceGroup == nil {
		t.Fatal("expected Alice's group to have 2 emails")
	}
}

func TestSyncAuthors(t *testing.T) {
	existing := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"alice": {
				DisplayName: "Alice",
				Emails:      []string{"alice@work.com"},
			},
		},
	}

	discovered := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"alice": {
				DisplayName: "Alice",
				Emails:      []string{"alice@work.com"}, // already known
			},
			"bob": {
				DisplayName: "Bob",
				Emails:      []string{"bob@example.com"}, // new
			},
		},
	}

	changed := SyncAuthors(existing, discovered)
	if !changed {
		t.Error("expected sync to report changes")
	}

	if len(existing.Authors) != 2 {
		t.Errorf("expected 2 authors after sync, got %d", len(existing.Authors))
	}

	// Alice should be unchanged.
	if len(existing.Authors["alice"].Emails) != 1 {
		t.Error("alice's emails should not have changed")
	}

	// Bob should be added.
	found := false
	for _, identity := range existing.Authors {
		for _, email := range identity.Emails {
			if email == "bob@example.com" {
				found = true
			}
		}
	}
	if !found {
		t.Error("bob@example.com should have been added")
	}
}

func TestSyncAuthors_NoChanges(t *testing.T) {
	existing := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"alice": {
				DisplayName: "Alice",
				Emails:      []string{"alice@work.com"},
			},
		},
	}

	discovered := &AuthorConfig{
		Authors: map[string]AuthorIdentity{
			"alice": {
				DisplayName: "Alice",
				Emails:      []string{"alice@work.com"},
			},
		},
	}

	changed := SyncAuthors(existing, discovered)
	if changed {
		t.Error("expected no changes")
	}
}

func TestGenerateAuthorID(t *testing.T) {
	used := map[string]bool{}

	tests := []struct {
		name string
		want string
	}{
		{"Lance Rogers", "lance-rogers"},
		{"bob", "bob"},
		{"Guild Test", "guild-test"},
		{"", "unknown"},
	}

	for _, tt := range tests {
		got := generateAuthorID(tt.name, used)
		if got != tt.want {
			t.Errorf("generateAuthorID(%q) = %q, want %q", tt.name, got, tt.want)
		}
		used[got] = true
	}

	// Duplicate name should get suffix.
	got := generateAuthorID("bob", used)
	if got != "bob-2" {
		t.Errorf("duplicate generateAuthorID(bob) = %q, want bob-2", got)
	}
}
