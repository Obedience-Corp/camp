package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fixtures", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

func TestValidateDocument_ValidAndInvalidFixtures(t *testing.T) {
	if err := ValidateDocument("", fixture(t, "valid-layout.json")); err != nil {
		t.Fatalf("valid-layout: %v", err)
	}
	if err := ValidateDocument("", fixture(t, "agent-metadata.json")); err != nil {
		t.Fatalf("agent-metadata: %v", err)
	}
	if err := ValidateDocument("", fixture(t, "invalid-tree.json")); err == nil {
		t.Fatal("invalid-tree: expected error")
	}
}

func TestValidateDocument_RejectsFutureVersion(t *testing.T) {
	raw := []byte(`{"version":99,"workspaceTabs":[]}`)
	err := ValidateDocument("", raw)
	if err == nil {
		t.Fatal("expected future version rejection")
	}
	if !strings.Contains(err.Error(), "unsupported workspace document version") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateDocument_AcceptsEmptyNullLegacyAndModernEmpty(t *testing.T) {
	cases := []struct {
		name    string
		raw     []byte
		wantErr bool
	}{
		{name: "missing tabs with paneTree", raw: []byte(`{"version":2,"paneTree":{"id":"p","type":"pane","content":{"type":"empty"}}}`)},
		{name: "empty tabs with paneTree", raw: []byte(`{"version":2,"workspaceTabs":[],"paneTree":{"id":"p","type":"pane","content":{"type":"empty"}}}`)},
		{name: "null tabs with paneTree", raw: []byte(`{"version":2,"workspaceTabs":null,"paneTree":{"id":"p","type":"pane","content":{"type":"empty"}}}`)},
		{name: "modern empty array", raw: []byte(`{"version":2,"workspaceTabs":[]}`)},
		{name: "modern null tabs", raw: []byte(`{"version":2,"workspaceTabs":null}`)},
		{name: "empty openFiles only", raw: []byte(`{"version":2,"workspaceTabs":[],"openFiles":[]}`)},
		{name: "missing tabs no legacy", raw: []byte(`{"version":2}`), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDocument("", tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidatePushSpec_StripsRuntimeAndRequiresURL(t *testing.T) {
	var spec PushSpec
	if err := json.Unmarshal(fixture(t, "push-spec-valid.json"), &spec); err != nil {
		t.Fatal(err)
	}
	// Structural validation without camp-root path checks.
	if err := ValidatePushSpec("", &spec); err != nil {
		t.Fatalf("valid push: %v", err)
	}
	if strings.Contains(string(spec.PaneTree), "browserTabId") {
		t.Fatalf("runtime browserTabId must be stripped, got %s", spec.PaneTree)
	}

	missingURL := PushSpec{
		ID:    "web",
		Title: "Web",
		PaneTree: json.RawMessage(`{
			"id":"p","type":"pane","content":{"type":"webview"}
		}`),
	}
	if err := ValidatePushSpec("", &missingURL); err == nil {
		t.Fatal("expected webview without url to fail")
	}
}

func TestCanonicalizeCampPath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	rel, err := CanonicalizeCampPath(root, "docs/README.md")
	if err != nil || rel != "docs/README.md" {
		t.Fatalf("rel=%q err=%v", rel, err)
	}
	if _, err := CanonicalizeCampPath(root, "../escape.md"); err == nil {
		t.Fatal("expected escape rejection")
	}
	abs := filepath.Join(root, "docs", "README.md")
	rel, err = CanonicalizeCampPath(root, abs)
	if err != nil || rel != "docs/README.md" {
		t.Fatalf("abs rel=%q err=%v", rel, err)
	}
}

func TestValidatePushSpec_RejectsOutOfBoundsWithCampRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	var bad PushSpec
	if err := json.Unmarshal(fixture(t, "push-spec-out-of-bounds.json"), &bad); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePushSpec(root, &bad); err == nil {
		t.Fatal("expected out-of-bounds rejection")
	}
}
