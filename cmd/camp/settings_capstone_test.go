package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/settings"
)

// TestMenuNeverRoutesToHiddenOrSecret enforces R2 at the menu layer: every row
// the menu offers for the real catalog resolves to a Structured or ReadOnly
// entry, so the router can never reach a Hidden or Secret entry.
func TestMenuNeverRoutesToHiddenOrSecret(t *testing.T) {
	entries, err := settings.BuildCatalog(context.Background(), "/campaign")
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	byID := make(map[string]settings.SettingEntry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}

	for _, scope := range []settings.Scope{settings.ScopeLocal, settings.ScopeGlobal} {
		for _, opt := range scopeOptions(entries, scope) {
			if opt.Value == valSeparator || opt.Value == valBack {
				continue
			}
			e, ok := byID[opt.Value]
			if !ok {
				t.Errorf("menu row %q has no catalog entry", opt.Value)
				continue
			}
			if e.Edit != settings.EditStructured && e.Edit != settings.EditReadOnly {
				t.Errorf("menu row %q has edit %d; only Structured/ReadOnly may appear", e.ID, e.Edit)
			}
		}
	}
}

// TestEditEntry_HiddenAndSecretAreInert is defense in depth: even if a Hidden or
// Secret entry were somehow handed to the router, it opens no editor and returns
// cleanly to the menu.
func TestEditEntry_HiddenAndSecretAreInert(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	ctx := context.Background()
	cases := []settings.SettingEntry{
		{ID: "project-registry", Title: "Managed file", Scope: settings.ScopeLocal, Edit: settings.EditHidden, Path: ".campaign/leverage/config.json"},
		{ID: "obey_env", Title: "Env secret", Scope: settings.ScopeGlobal, Edit: settings.EditSecret, Path: "/home/u/.obey/.env"},
	}
	for _, e := range cases {
		out := captureStdout(t, func() {
			if err := editEntry(ctx, e, "/home/u/camp"); err != nil {
				t.Fatalf("editEntry(%s) returned error: %v", e.ID, err)
			}
		})
		if !strings.Contains(out, "not editable") {
			t.Errorf("editEntry(%s) output %q should show the not-editable message", e.ID, out)
		}
	}
}
