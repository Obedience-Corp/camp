package artifacts

import "testing"

// The refusal list is the safety boundary: every entry exists because
// auto-declaring there would produce a state the rest of the package calls
// invalid. Error cases first.
func TestResolveAutoRootRefusals(t *testing.T) {
	cases := []struct {
		name string
		file string
		want RefusalReason
	}{
		{
			name: "file at the campaign root would make the campaign an artifact root",
			file: "render.mov",
			want: RefusalCampaignRoot,
		},
		{
			name: "path inside camp's own state tree",
			file: ".campaign/graphs/campaign-graph.png",
			want: RefusalCampaignState,
		},
		{
			name: "the state directory itself",
			file: ".campaign",
			want: RefusalCampaignState,
		},
		{
			name: "state tree is matched case-insensitively",
			file: ".Campaign/cache/big.bin",
			want: RefusalCampaignState,
		},
		{
			name: "path escaping the campaign",
			file: "../outside/huge.bin",
			want: RefusalEscapesCampaign,
		},
		{
			name: "empty path",
			file: "",
			want: RefusalEscapesCampaign,
		},
		{
			name: "dot path",
			file: ".",
			want: RefusalEscapesCampaign,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveAutoRoot(&File{Version: 1}, t.TempDir(), tc.file)
			if got.Refusal != tc.want {
				t.Errorf("ResolveAutoRoot(%q).Refusal = %q, want %q", tc.file, got.Refusal, tc.want)
			}
			if got.Path != "" {
				t.Errorf("ResolveAutoRoot(%q).Path = %q, want empty on refusal", tc.file, got.Path)
			}
		})
	}
}

func TestResolveAutoRootChoosesParent(t *testing.T) {
	cases := []struct {
		name string
		file string
		want string
	}{
		{
			name: "parent directory of the offending file",
			file: "videos/my-video/footage.mp4",
			want: "videos/my-video",
		},
		{
			name: "single-level directory",
			file: "media/huge.bin",
			want: "media",
		},
		{
			name: "deeply nested",
			file: "a/b/c/d/huge.bin",
			want: "a/b/c/d",
		},
		{
			name: "dot-prefixed spelling normalizes",
			file: "./media/huge.bin",
			want: "media",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveAutoRoot(&File{Version: 1}, t.TempDir(), tc.file)
			if got.Refusal != RefusalNone {
				t.Fatalf("ResolveAutoRoot(%q) refused with %q", tc.file, got.Refusal)
			}
			if got.Path != tc.want {
				t.Errorf("ResolveAutoRoot(%q).Path = %q, want %q", tc.file, got.Path, tc.want)
			}
			if !got.Declared() {
				t.Error("Declared() = false, want true for a fresh root")
			}
		})
	}
}

// Criterion 10: a file already inside a declared root reuses that root rather
// than declaring a nested sibling inside it.
func TestResolveAutoRootReusesEnclosingRoot(t *testing.T) {
	cfg := &File{Version: 1, Roots: []Root{{Path: "videos"}}}

	cases := []struct {
		name string
		file string
		want string
	}{
		{name: "directly inside the declared root", file: "videos/footage.mp4", want: "videos"},
		{name: "nested below the declared root", file: "videos/2026/q3/footage.mp4", want: "videos"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveAutoRoot(cfg, t.TempDir(), tc.file)
			if got.Refusal != RefusalNone {
				t.Fatalf("refused with %q", got.Refusal)
			}
			if got.Path != tc.want {
				t.Errorf("Path = %q, want the enclosing root %q", got.Path, tc.want)
			}
			if !got.Existing {
				t.Error("Existing = false, want true for an already-declared root")
			}
			if got.Declared() {
				t.Error("Declared() = true; an existing root needs no new declaration")
			}
		})
	}
}

// A root that merely shares a name prefix is not enclosing.
func TestResolveAutoRootPrefixIsNotEnclosing(t *testing.T) {
	cfg := &File{Version: 1, Roots: []Root{{Path: "videos"}}}

	got := ResolveAutoRoot(cfg, t.TempDir(), "videos-archive/footage.mp4")
	if got.Existing {
		t.Error("Existing = true; 'videos' must not enclose 'videos-archive'")
	}
	if got.Path != "videos-archive" {
		t.Errorf("Path = %q, want %q", got.Path, "videos-archive")
	}
}

func TestSiblingRootCount(t *testing.T) {
	cfg := &File{Version: 1, Roots: []Root{
		{Path: "videos/one"},
		{Path: "videos/two"},
		{Path: "videos/three"},
		{Path: "media/other"},
		{Path: "toplevel"},
	}}

	cases := []struct {
		parent string
		want   int
	}{
		{parent: "videos", want: 3},
		{parent: "media", want: 1},
		{parent: "", want: 0},
		{parent: "absent", want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.parent, func(t *testing.T) {
			if got := SiblingRootCount(cfg, tc.parent); got != tc.want {
				t.Errorf("SiblingRootCount(%q) = %d, want %d", tc.parent, got, tc.want)
			}
		})
	}
}

func TestSiblingRootCountNilConfig(t *testing.T) {
	if got := SiblingRootCount(nil, "videos"); got != 0 {
		t.Errorf("SiblingRootCount(nil, _) = %d, want 0", got)
	}
}
