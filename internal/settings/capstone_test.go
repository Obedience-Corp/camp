package settings

import (
	"context"
	"strings"
	"testing"
)

// TestHiddenEntryNeedsNoMenuCode demonstrates DP1: adding a Hidden or ReadOnly
// catalog entry is pure data. ForScope filters only on Scope + Listable, so a
// new Hidden entry is excluded and a ReadOnly entry is shown (view-only) with no
// change to the menu code in cmd/camp/settings.go.
func TestHiddenEntryNeedsNoMenuCode(t *testing.T) {
	cat := []SettingEntry{
		{ID: "new_managed_file", Scope: ScopeLocal, Edit: EditHidden, Path: ".campaign/whatever.json"},
		{ID: "readonly_file", Scope: ScopeLocal, Edit: EditReadOnly, Path: ".campaign/readonly.json"},
		{ID: "campaign_manifest", Scope: ScopeLocal, Edit: EditStructured, Path: ".campaign/campaign.yaml"},
	}

	listable := indexByID(ForScope(cat, ScopeLocal))
	if _, ok := listable["new_managed_file"]; ok {
		t.Error("a Hidden entry must never be listable")
	}
	if _, ok := listable["readonly_file"]; !ok {
		t.Error("a ReadOnly entry should be listable (view-only)")
	}
	if _, ok := listable["campaign_manifest"]; !ok {
		t.Error("a Structured entry should be listable")
	}

	for _, e := range cat {
		if e.Edit == EditHidden && e.Editable() {
			t.Errorf("Hidden entry %q must not be editable", e.ID)
		}
	}
}

// TestSecretsNeverListedOrEditable enforces R2 against the real catalog: every
// EditSecret entry is neither editable nor listable in any scope, and the obey
// environment file is classified as a secret.
func TestSecretsNeverListedOrEditable(t *testing.T) {
	entries, err := BuildCatalog(context.Background(), "/campaign")
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}

	secretsSeen := 0
	for _, e := range entries {
		if e.Edit != EditSecret {
			continue
		}
		secretsSeen++
		if e.Editable() || e.Listable() {
			t.Errorf("secret %q must be neither editable nor listable", e.ID)
		}
	}
	if secretsSeen == 0 {
		t.Fatal("expected at least one secret entry in the catalog")
	}

	env, ok := indexByID(entries)["obey_env"]
	if !ok || env.Edit != EditSecret || !strings.HasSuffix(env.Path, ".env") {
		t.Errorf("obey env entry = %+v, want an EditSecret .env entry", env)
	}

	for _, scope := range []Scope{ScopeLocal, ScopeGlobal} {
		for _, e := range ForScope(entries, scope) {
			if e.Edit == EditSecret || e.Edit == EditHidden {
				t.Errorf("non-listable entry %q (edit %d) leaked into scope %d menu", e.ID, e.Edit, scope)
			}
		}
	}
}
