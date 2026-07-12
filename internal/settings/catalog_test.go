package settings

import (
	"context"
	"testing"
)

func indexByID(entries []SettingEntry) map[string]SettingEntry {
	m := make(map[string]SettingEntry, len(entries))
	for _, e := range entries {
		m[e.ID] = e
	}
	return m
}

func indexByPath(entries []SettingEntry) map[string]SettingEntry {
	m := make(map[string]SettingEntry, len(entries))
	for _, e := range entries {
		m[e.Path] = e
	}
	return m
}

func TestBuildCatalog_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BuildCatalog(ctx, "/campaign"); err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestBuildCatalog_StructuredEntries(t *testing.T) {
	entries, err := BuildCatalog(context.Background(), "/campaign")
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	byID := indexByID(entries)

	want := []struct {
		id     string
		scope  Scope
		edit   Edit
		format Format
	}{
		{"campaign_manifest", ScopeLocal, EditStructured, FormatYAML},
		{"local_settings", ScopeLocal, EditStructured, FormatJSON},
		{"allowlist", ScopeLocal, EditStructured, FormatJSON},
		{"registry", ScopeGlobal, EditStructured, FormatJSON},
		{"global_config", ScopeGlobal, EditStructured, FormatJSON},
	}
	for _, w := range want {
		e, ok := byID[w.id]
		if !ok {
			t.Errorf("missing structured entry %q", w.id)
			continue
		}
		if e.Scope != w.scope || e.Edit != w.edit || e.Format != w.format {
			t.Errorf("entry %q = {scope:%d edit:%d format:%d}, want {scope:%d edit:%d format:%d}",
				w.id, e.Scope, e.Edit, e.Format, w.scope, w.edit, w.format)
		}
		if !e.Editable() || !e.Listable() {
			t.Errorf("structured entry %q must be editable and listable", w.id)
		}
	}
}

func TestBuildCatalog_HiddenFromContract(t *testing.T) {
	entries, err := BuildCatalog(context.Background(), "/campaign")
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	byPath := indexByPath(entries)

	// Managed files declared in the watcher contract appear as Hidden and are
	// never listable, so they can never be surfaced as editable.
	hiddenPaths := []string{
		".campaign/leverage/config.json",
		".campaign/settings/pins.json",
		".campaign/settings/jumps.yaml",
	}
	for _, p := range hiddenPaths {
		e, ok := byPath[p]
		if !ok {
			t.Errorf("expected contract-managed file %q in catalog", p)
			continue
		}
		if e.Edit != EditHidden {
			t.Errorf("contract file %q Edit = %d, want EditHidden(%d)", p, e.Edit, EditHidden)
		}
		if e.Listable() || e.Editable() {
			t.Errorf("hidden file %q must be neither listable nor editable", p)
		}
	}

	// Files that have structured editors must not be duplicated as Hidden.
	for _, p := range []string{".campaign/campaign.yaml", ".campaign/settings/allowlist.json"} {
		e, ok := byPath[p]
		if !ok {
			t.Errorf("expected structured file %q in catalog", p)
			continue
		}
		if e.Edit == EditHidden {
			t.Errorf("structured file %q must not be Hidden", p)
		}
	}
}

func TestBuildCatalog_SecretEntries(t *testing.T) {
	entries, err := BuildCatalog(context.Background(), "/campaign")
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	byID := indexByID(entries)

	for _, id := range []string{"obey_env", "obey_agent_gh_token"} {
		e, ok := byID[id]
		if !ok {
			t.Fatalf("missing secret entry %q", id)
		}
		if e.Edit != EditSecret {
			t.Errorf("secret %q Edit = %d, want EditSecret(%d)", id, e.Edit, EditSecret)
		}
		if e.Editable() || e.Listable() {
			t.Errorf("secret %q must be neither editable nor listable", id)
		}
	}
}

func TestForScope_OnlyListableInScope(t *testing.T) {
	entries, err := BuildCatalog(context.Background(), "/campaign")
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}

	cases := []struct {
		scope   Scope
		wantIDs []string
	}{
		{ScopeLocal, []string{"campaign_manifest", "local_settings", "allowlist"}},
		{ScopeGlobal, []string{"registry", "global_config"}},
	}
	for _, c := range cases {
		got := ForScope(entries, c.scope)
		gotIDs := indexByID(got)
		for _, id := range c.wantIDs {
			if _, ok := gotIDs[id]; !ok {
				t.Errorf("ForScope(%d) missing %q", c.scope, id)
			}
		}
		for _, e := range got {
			if e.Scope != c.scope {
				t.Errorf("ForScope(%d) returned wrong-scope entry %q (scope %d)", c.scope, e.ID, e.Scope)
			}
			if e.Edit == EditHidden || e.Edit == EditSecret {
				t.Errorf("ForScope(%d) leaked non-listable entry %q (edit %d)", c.scope, e.ID, e.Edit)
			}
		}
	}

	// Secrets are Global scope but must never appear in the global menu.
	for _, e := range ForScope(entries, ScopeGlobal) {
		if e.ID == "obey_env" || e.ID == "obey_agent_gh_token" {
			t.Errorf("secret %q leaked into global menu", e.ID)
		}
	}
}

func TestSettingEntry_EditableListable(t *testing.T) {
	tests := []struct {
		edit         Edit
		wantEditable bool
		wantListable bool
	}{
		{EditStructured, true, true},
		{EditReadOnly, false, true},
		{EditHidden, false, false},
		{EditSecret, false, false},
	}
	for _, tt := range tests {
		e := SettingEntry{Edit: tt.edit}
		if got := e.Editable(); got != tt.wantEditable {
			t.Errorf("Edit %d Editable() = %v, want %v", tt.edit, got, tt.wantEditable)
		}
		if got := e.Listable(); got != tt.wantListable {
			t.Errorf("Edit %d Listable() = %v, want %v", tt.edit, got, tt.wantListable)
		}
	}
}
