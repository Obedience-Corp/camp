package skills

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func shippedFixture() []ShippedFile {
	return []ShippedFile{
		{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")},
		{Bundle: "beta", Path: "SKILL.md", Content: []byte("beta v2\n")},
		{Bundle: "gamma", Path: "SKILL.md", Content: []byte("gamma v2\n")},
		{Bundle: "gamma", Path: "notes/usage.md", Content: []byte("usage v2\n")},
		{Bundle: "delta", Path: "SKILL.md", Content: []byte("delta v2\n")},
		{Bundle: "omega", Path: "SKILL.md", Content: []byte("omega v2\n")},
	}
}

func manifestYAML(records map[string]map[string]string) string {
	var b strings.Builder
	b.WriteString("schema_version: " + ManifestSchemaVersion + "\ncamp_version: v1\nbundles:\n")
	for bundle, files := range records {
		b.WriteString("  " + bundle + ":\n")
		for rel, hash := range files {
			b.WriteString("    " + rel + ": " + hash + "\n")
		}
	}
	return b.String()
}

func skillsFSFixture() fstest.MapFS {
	return fstest.MapFS{
		"alpha/SKILL.md":        {Data: []byte("alpha v2\n")},
		"beta/SKILL.md":         {Data: []byte("beta v1\n")},
		"gamma/SKILL.md":        {Data: []byte("gamma v1 plus my edit\n")},
		"delta/SKILL.md":        {Data: []byte("delta local\n")},
		"alpha/references/x.md": {Data: []byte("mine\n")},
		"staff-review/SKILL.md": {Data: []byte("local bundle\n")},
		ManifestFileName: {Data: []byte(manifestYAML(map[string]map[string]string{
			"beta":  {"SKILL.md": HashContent([]byte("beta v1\n"))},
			"gamma": {"SKILL.md": HashContent([]byte("gamma v1\n")), "notes/usage.md": HashContent([]byte("usage v1\n"))},
		}))},
	}
}

func TestClassify_FileStates(t *testing.T) {
	inv, err := Classify(t.Context(), skillsFSFixture(), shippedFixture(), nil)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	want := map[string]ShippedState{
		".campaign/skills/alpha/SKILL.md":       ShippedCurrent,
		".campaign/skills/beta/SKILL.md":        ShippedOutdated,
		".campaign/skills/gamma/SKILL.md":       ShippedModified,
		".campaign/skills/gamma/notes/usage.md": ShippedMissing,
		".campaign/skills/delta/SKILL.md":       ShippedUnknown,
		".campaign/skills/omega/SKILL.md":       ShippedMissing,
	}
	if len(inv.Files) != len(want) {
		t.Fatalf("Classify() returned %d files, want %d (unshipped files must not be listed)", len(inv.Files), len(want))
	}
	for _, f := range inv.Files {
		if f.State != want[f.RelPath()] {
			t.Errorf("%s state = %q, want %q", f.RelPath(), f.State, want[f.RelPath()])
		}
	}
}

func TestClassify_WithoutManifestDifferingFilesAreUnknown(t *testing.T) {
	fsys := skillsFSFixture()
	delete(fsys, ManifestFileName)
	inv, err := Classify(t.Context(), fsys, shippedFixture(), nil)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if inv.Manifest != nil {
		t.Fatalf("Manifest = %+v, want nil without a manifest file", inv.Manifest)
	}
	for _, f := range inv.Files {
		if f.Bundle == "beta" && f.State != ShippedUnknown {
			t.Errorf("beta without a manifest = %q, want unknown: an unrecorded difference may be a local edit", f.State)
		}
	}
}

func TestClassify_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Classify(ctx, skillsFSFixture(), shippedFixture(), nil); err == nil {
		t.Fatal("Classify() with a canceled context returned no error")
	}
}

func TestClassify_UnreadableManifestIsAnError(t *testing.T) {
	fsys := skillsFSFixture()
	fsys[ManifestFileName] = &fstest.MapFile{Data: []byte("bundles: [not, a, map\n")}
	_, err := Classify(t.Context(), fsys, shippedFixture(), nil)
	if err == nil || !strings.Contains(err.Error(), "remove it to rebuild") {
		t.Fatalf("Classify() error = %v, want a parse error naming the remedy", err)
	}
}

func TestInventoryBundles_PreservedStatesOutrankRefreshable(t *testing.T) {
	inv, err := Classify(t.Context(), skillsFSFixture(), shippedFixture(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ShippedState{}
	var order []string
	for _, b := range inv.Bundles() {
		got[b.Slug] = b.State
		order = append(order, b.Slug)
	}
	want := map[string]ShippedState{
		"alpha": ShippedCurrent,
		"beta":  ShippedOutdated,
		"gamma": ShippedModified,
		"delta": ShippedUnknown,
		"omega": ShippedMissing,
	}
	for slug, state := range want {
		if got[slug] != state {
			t.Errorf("bundle %s = %q, want %q", slug, got[slug], state)
		}
	}
	if strings.Join(order, ",") != "alpha,beta,gamma,delta,omega" {
		t.Errorf("bundle order = %v, want shipped order", order)
	}
}

func TestNextManifest_RecordsWrittenAndKeepsPreservedRecords(t *testing.T) {
	inv, err := Classify(t.Context(), skillsFSFixture(), shippedFixture(), nil)
	if err != nil {
		t.Fatal(err)
	}
	next := inv.NextManifest("v2", map[string]bool{
		".campaign/skills/beta/SKILL.md":  true,
		".campaign/skills/omega/SKILL.md": true,
	})
	if next.CampVersion != "v2" || next.SchemaVersion != ManifestSchemaVersion {
		t.Fatalf("header = %q %q", next.SchemaVersion, next.CampVersion)
	}
	checks := []struct {
		bundle, rel, want string
		present           bool
	}{
		{"alpha", "SKILL.md", HashContent([]byte("alpha v2\n")), true},
		{"beta", "SKILL.md", HashContent([]byte("beta v2\n")), true},
		{"omega", "SKILL.md", HashContent([]byte("omega v2\n")), true},
		{"gamma", "SKILL.md", HashContent([]byte("gamma v1\n")), true},
		{"gamma", "notes/usage.md", HashContent([]byte("usage v1\n")), true},
		{"delta", "SKILL.md", "", false},
	}
	for _, c := range checks {
		got, ok := next.recorded(c.bundle, c.rel)
		if ok != c.present || got != c.want {
			t.Errorf("record %s/%s = %q (present %v), want %q (present %v)", c.bundle, c.rel, got, ok, c.want, c.present)
		}
	}
	if _, ok := next.Bundles["staff-review"]; ok {
		t.Error("manifest recorded a bundle camp does not ship")
	}
}

func TestManifestStale(t *testing.T) {
	shipped := []ShippedFile{{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")}}
	current := fstest.MapFS{"alpha/SKILL.md": {Data: []byte("alpha v2\n")}}

	inv, err := Classify(t.Context(), current, shipped, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !inv.ManifestStale() {
		t.Error("a camp without a manifest must report it stale so repair records one")
	}

	current[ManifestFileName] = &fstest.MapFile{Data: []byte(manifestYAML(map[string]map[string]string{
		"alpha": {"SKILL.md": HashContent([]byte("alpha v2\n"))},
	}))}
	if inv, err = Classify(t.Context(), current, shipped, nil); err != nil {
		t.Fatal(err)
	}
	if inv.ManifestStale() {
		t.Error("a manifest matching every current file must not be stale")
	}

	current["alpha/SKILL.md"] = &fstest.MapFile{Data: []byte("alpha edited\n")}
	if inv, err = Classify(t.Context(), current, shipped, nil); err != nil {
		t.Fatal(err)
	}
	if inv.ManifestStale() {
		t.Error("a preserved edit keeps its old record; the manifest must not be reported stale")
	}
}

func TestScopeBundles_UnknownSlugListsShippedBundles(t *testing.T) {
	_, err := scopeBundles(shippedFixture(), []string{"alpha", "nope"})
	if err == nil {
		t.Fatal("scopeBundles() accepted an unknown slug")
	}
	for _, want := range []string{"nope", "alpha, beta, delta, gamma, omega"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}

	scope, err := scopeBundles(shippedFixture(), nil)
	if err != nil || len(scope) != 5 {
		t.Fatalf("scopeBundles(nil) = %v, %v; want every shipped bundle", scope, err)
	}
}

func TestActionFor(t *testing.T) {
	cases := []struct {
		state ShippedState
		force bool
		want  FileAction
	}{
		{ShippedMissing, false, ActionAdd},
		{ShippedOutdated, false, ActionUpdate},
		{ShippedCurrent, true, ActionKeep},
		{ShippedModified, false, ActionPreserve},
		{ShippedUnknown, false, ActionPreserve},
		{ShippedModified, true, ActionReplace},
		{ShippedUnknown, true, ActionReplace},
	}
	for _, c := range cases {
		if got := actionFor(c.state, c.force); got != c.want {
			t.Errorf("actionFor(%q, force=%v) = %q, want %q", c.state, c.force, got, c.want)
		}
	}
}

func TestClassify_SymlinkedFilesAreNeverReplaced(t *testing.T) {
	shipped := []ShippedFile{
		{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")},
		{Bundle: "beta", Path: "SKILL.md", Content: []byte("beta v2\n")},
		{Bundle: "gamma", Path: "SKILL.md", Content: []byte("gamma v2\n")},
		{Bundle: "delta", Path: "SKILL.md", Content: []byte("delta v2\n")},
	}
	fsys := fstest.MapFS{
		"shared/alpha.md":      {Data: []byte("alpha v1\n")},
		"shared/gamma.md":      {Data: []byte("gamma v2\n")},
		"shared/beta/SKILL.md": {Data: []byte("beta v1\n")},
		"alpha/SKILL.md":       {Data: []byte("../shared/alpha.md"), Mode: fs.ModeSymlink},
		"beta":                 {Data: []byte("shared/beta"), Mode: fs.ModeSymlink},
		"gamma/SKILL.md":       {Data: []byte("../shared/gamma.md"), Mode: fs.ModeSymlink},
		"delta/SKILL.md":       {Data: []byte("../shared/gone.md"), Mode: fs.ModeSymlink},
		ManifestFileName: {Data: []byte(manifestYAML(map[string]map[string]string{
			"alpha": {"SKILL.md": HashContent([]byte("alpha v1\n"))},
		}))},
	}
	inv, err := Classify(t.Context(), fsys, shipped, nil)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	want := map[string]ShippedState{
		"alpha": ShippedSymlink,
		"beta":  ShippedSymlink,
		"gamma": ShippedCurrent,
		"delta": ShippedSymlink,
	}
	for _, f := range inv.Files {
		if f.State != want[f.Bundle] {
			t.Errorf("%s state = %q, want %q", f.RelPath(), f.State, want[f.Bundle])
		}
	}
	if got := actionFor(ShippedSymlink, true); got != ActionPreserve {
		t.Errorf("actionFor(symlink, force) = %q, want preserve: --force never writes through a symlink", got)
	}
}
