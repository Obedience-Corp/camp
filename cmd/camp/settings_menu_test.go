package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"

	"github.com/Obedience-Corp/camp/internal/settings"
)

func fixtureCatalog() []settings.SettingEntry {
	return []settings.SettingEntry{
		{ID: "struct_local", Title: "Struct Local", Scope: settings.ScopeLocal, Edit: settings.EditStructured},
		{ID: "ro_local", Title: "RO Local", Scope: settings.ScopeLocal, Edit: settings.EditReadOnly},
		{ID: "hidden_local", Title: "Hidden Local", Scope: settings.ScopeLocal, Edit: settings.EditHidden},
		{ID: "struct_global", Title: "Struct Global", Scope: settings.ScopeGlobal, Edit: settings.EditStructured},
		{ID: "secret_global", Title: "Secret Global", Scope: settings.ScopeGlobal, Edit: settings.EditSecret},
	}
}

func optionValues(opts []huh.Option[string]) []string {
	vals := make([]string, len(opts))
	for i, o := range opts {
		vals[i] = o.Value
	}
	return vals
}

func TestScopeOptions_Composition(t *testing.T) {
	tests := []struct {
		name  string
		scope settings.Scope
		want  []string
	}{
		{"local lists structured and read-only, then separator and back", settings.ScopeLocal, []string{"struct_local", "ro_local", valSeparator, valBack}},
		{"global lists structured, then separator and back", settings.ScopeGlobal, []string{"struct_global", valSeparator, valBack}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := optionValues(scopeOptions(fixtureCatalog(), tt.scope))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("scopeOptions values = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScopeOptions_ExcludesHiddenAndSecret(t *testing.T) {
	for _, scope := range []settings.Scope{settings.ScopeLocal, settings.ScopeGlobal} {
		for _, v := range optionValues(scopeOptions(fixtureCatalog(), scope)) {
			if v == "hidden_local" || v == "secret_global" {
				t.Errorf("scope %d menu leaked non-listable entry %q", scope, v)
			}
		}
	}
}

func TestEntryByID(t *testing.T) {
	cat := fixtureCatalog()
	if e, ok := entryByID(cat, "struct_global"); !ok || e.Title != "Struct Global" {
		t.Errorf("entryByID(struct_global) = %+v, ok=%v", e, ok)
	}
	if _, ok := entryByID(cat, "does_not_exist"); ok {
		t.Error("entryByID(does_not_exist) should report not found")
	}
}

func TestScopeHeader(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	t.Setenv("XDG_CONFIG_HOME", "")

	gTitle, gDesc := scopeHeader(settings.ScopeGlobal, "/campaign/root")
	if gTitle != "Global Settings" {
		t.Errorf("global title = %q", gTitle)
	}
	if gDesc != "Files under ~/.obey/campaign/" {
		t.Errorf("global desc = %q, want %q", gDesc, "Files under ~/.obey/campaign/")
	}

	lTitle, lDesc := scopeHeader(settings.ScopeLocal, "/campaign/root")
	if lTitle != "Local Settings (this campaign)" {
		t.Errorf("local title = %q", lTitle)
	}
	if lDesc != "Files under .campaign/" {
		t.Errorf("local desc = %q, want %q", lDesc, "Files under .campaign/")
	}
}

// The editor headers derive from CatalogPath: root-relative for local files and
// tilde-collapsed for global files.
func TestCatalogPathHeaders(t *testing.T) {
	t.Setenv("HOME", "/home/u")

	local := settings.SettingEntry{Scope: settings.ScopeLocal, Path: ".campaign/campaign.yaml"}
	if got := settings.CatalogPath(local, "/home/u/camp"); got != ".campaign/campaign.yaml" {
		t.Errorf("local header path = %q, want %q", got, ".campaign/campaign.yaml")
	}

	global := settings.SettingEntry{Scope: settings.ScopeGlobal, Path: "/home/u/.obey/campaign/registry.json"}
	if got := settings.CatalogPath(global, ""); got != "~/.obey/campaign/registry.json" {
		t.Errorf("global header path = %q, want %q", got, "~/.obey/campaign/registry.json")
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return buf.String()
}

// Entries without an implemented editor (later-sequence stubs and unknown IDs)
// must return to the menu (nil error) and surface a clear message rather than
// crash or open a form.
func TestEditEntry_UnimplementedReturnsToMenu(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	ctx := context.Background()

	tests := []struct {
		name       string
		entry      settings.SettingEntry
		wantSubstr string
	}{
		{"unknown id shows generic message", settings.SettingEntry{ID: "mystery", Title: "Mystery", Scope: settings.ScopeLocal}, "not editable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				if err := editEntry(ctx, tt.entry, "/home/u/camp"); err != nil {
					t.Fatalf("editEntry returned error: %v", err)
				}
			})
			if !strings.Contains(out, tt.wantSubstr) {
				t.Errorf("editEntry output %q missing %q", out, tt.wantSubstr)
			}
		})
	}
}
