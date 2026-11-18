package skills

import (
	"io/fs"
	"maps"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"
)

const pluginSkill = "---\nname: festival-app\ndescription: Drive the Festival app\n---\n"

// A manifest with no plugin records must encode exactly as camp wrote it
// before plugin bundles existed, so camps that never use one see no change.
func TestManifest_WithoutProvidersEncodesAsBefore(t *testing.T) {
	m := &Manifest{
		SchemaVersion: ManifestSchemaVersion,
		CampVersion:   "v0.17.0",
		Bundles: map[string]map[string]string{
			"campaign-commit": {"SKILL.md": "aaa"},
			"camp-navigation": {"SKILL.md": "bbb", "notes/usage.md": "ccc"},
		},
	}
	got, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	want := "schema_version: skills-shipped/v1alpha1\n" +
		"camp_version: v0.17.0\n" +
		"bundles:\n" +
		"    camp-navigation:\n" +
		"        SKILL.md: bbb\n" +
		"        notes/usage.md: ccc\n" +
		"    campaign-commit:\n" +
		"        SKILL.md: aaa\n"
	if string(got) != want {
		t.Fatalf("manifest bytes changed:\n got: %q\nwant: %q", got, want)
	}
}

func TestManifest_ProvidersRoundTrip(t *testing.T) {
	data := "schema_version: skills-shipped/v1alpha1\n" +
		"camp_version: v0.17.0\n" +
		"bundles:\n" +
		"    festival-app:\n" +
		"        SKILL.md: ddd\n" +
		"providers:\n" +
		"    festival-app:\n" +
		"        plugin: camp-app\n" +
		"        version: 0.4.0\n" +
		"    other-app:\n" +
		"        plugin: camp-app\n" +
		"        declined: true\n"
	m, err := ReadManifest(fstest.MapFS{ManifestFileName: {Data: []byte(data)}})
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	want := map[string]ProviderRecord{
		"festival-app": {Plugin: "camp-app", Version: "0.4.0"},
		"other-app":    {Plugin: "camp-app", Declined: true},
	}
	if !maps.Equal(m.Providers, want) {
		t.Fatalf("Providers = %+v, want %+v", m.Providers, want)
	}
	got, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != data {
		t.Fatalf("round trip changed the manifest:\n got: %q\nwant: %q", got, data)
	}
}

func TestManifestSameRecords_ComparesProviders(t *testing.T) {
	base := func() *Manifest {
		return &Manifest{
			Bundles:   map[string]map[string]string{"festival-app": {"SKILL.md": "ddd"}},
			Providers: map[string]ProviderRecord{"festival-app": {Plugin: "camp-app", Version: "0.4.0"}},
		}
	}
	if !base().sameRecords(base()) {
		t.Fatal("identical manifests reported different")
	}
	for name, change := range map[string]func(*Manifest){
		"version":  func(m *Manifest) { m.Providers["festival-app"] = ProviderRecord{Plugin: "camp-app", Version: "0.5.0"} },
		"declined": func(m *Manifest) { m.Providers["festival-app"] = ProviderRecord{Plugin: "camp-app", Declined: true} },
		"dropped":  func(m *Manifest) { m.Providers = nil },
	} {
		other := base()
		change(other)
		if base().sameRecords(other) {
			t.Errorf("%s: a provider change was reported as the same records", name)
		}
	}
	if !(&Manifest{}).sameRecords(&Manifest{Providers: map[string]ProviderRecord{}}) {
		t.Error("an empty provider map differs from none")
	}
}

// pluginCampFS is a camp whose manifest holds a plugin bundle still on disk,
// a declined bundle, a plugin bundle someone deleted by hand, a bundle camp
// no longer ships, and a provider record that names a bundle camp ships.
func pluginCampFS() fstest.MapFS {
	data := manifestYAML(map[string]map[string]string{
		"beta":         {"SKILL.md": HashContent([]byte("beta v1\n"))},
		"festival-app": {"SKILL.md": HashContent([]byte(pluginSkill))},
		"gone-app":     {"SKILL.md": "eee"},
		"retired":      {"SKILL.md": "fff"},
		"alpha":        {"SKILL.md": HashContent([]byte("alpha v2\n"))},
	}) + "providers:\n" +
		"  festival-app:\n    plugin: camp-app\n    version: 0.4.0\n" +
		"  other-app:\n    plugin: camp-app\n    declined: true\n" +
		"  gone-app:\n    plugin: camp-app\n    version: 0.4.0\n" +
		"  alpha:\n    plugin: camp-app\n    version: 0.4.0\n" +
		"  ../escape:\n    plugin: camp-app\n    version: 0.4.0\n"
	return fstest.MapFS{
		"alpha/SKILL.md":        {Data: []byte("alpha v2\n")},
		"beta/SKILL.md":         {Data: []byte("beta v1\n")},
		"festival-app/SKILL.md": {Data: []byte(pluginSkill)},
		"retired/SKILL.md":      {Data: []byte("retired\n")},
		ManifestFileName:        {Data: []byte(data)},
	}
}

func TestNextManifest_KeepsPluginRecordsWhileTheBundleExistsOrIsDeclined(t *testing.T) {
	shipped := []ShippedFile{
		{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")},
		{Bundle: "beta", Path: "SKILL.md", Content: []byte("beta v2\n")},
	}
	inv, err := Classify(t.Context(), pluginCampFS(), shipped, nil)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	next := inv.NextManifest("v2", map[string]bool{".campaign/skills/beta/SKILL.md": true})

	wantBundles := map[string]map[string]string{
		"alpha":        {"SKILL.md": HashContent([]byte("alpha v2\n"))},
		"beta":         {"SKILL.md": HashContent([]byte("beta v2\n"))},
		"festival-app": {"SKILL.md": HashContent([]byte(pluginSkill))},
	}
	if !maps.EqualFunc(next.Bundles, wantBundles, maps.Equal) {
		t.Errorf("Bundles = %v, want %v", next.Bundles, wantBundles)
	}
	wantProviders := map[string]ProviderRecord{
		"festival-app": {Plugin: "camp-app", Version: "0.4.0"},
		"other-app":    {Plugin: "camp-app", Declined: true},
	}
	if !maps.Equal(next.Providers, wantProviders) {
		t.Errorf("Providers = %v, want %v", next.Providers, wantProviders)
	}
}

func TestManifestStale_PluginRecordsAloneAreNotStale(t *testing.T) {
	shipped := []ShippedFile{{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")}}
	data := manifestYAML(map[string]map[string]string{
		"alpha":        {"SKILL.md": HashContent([]byte("alpha v2\n"))},
		"festival-app": {"SKILL.md": HashContent([]byte(pluginSkill))},
	}) + "providers:\n" +
		"  festival-app:\n    plugin: camp-app\n    version: 0.4.0\n" +
		"  other-app:\n    plugin: camp-app\n    declined: true\n"
	fsys := fstest.MapFS{
		"alpha/SKILL.md":        {Data: []byte("alpha v2\n")},
		"festival-app/SKILL.md": {Data: []byte(pluginSkill)},
		ManifestFileName:        {Data: []byte(data)},
	}
	inv, err := Classify(t.Context(), fsys, shipped, nil)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if inv.ManifestStale() {
		t.Fatal("ManifestStale() = true for a camp whose only extra records are plugin records")
	}

	delete(fsys, "festival-app/SKILL.md")
	inv, err = Classify(t.Context(), fsys, shipped, nil)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if !inv.ManifestStale() {
		t.Fatal("ManifestStale() = false after the plugin bundle's directory was removed")
	}
}

type statFailFS struct {
	fstest.MapFS
	fail string
}

func (f statFailFS) Stat(name string) (fs.FileInfo, error) {
	if name == f.fail {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrPermission}
	}
	return f.MapFS.Stat(name)
}

func TestClassify_UnreadablePluginDirectoryKeepsItsRecord(t *testing.T) {
	shipped := []ShippedFile{{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")}}
	data := manifestYAML(map[string]map[string]string{
		"alpha":        {"SKILL.md": HashContent([]byte("alpha v2\n"))},
		"festival-app": {"SKILL.md": "ddd"},
	}) + "providers:\n  festival-app:\n    plugin: camp-app\n    version: 0.4.0\n"
	fsys := statFailFS{MapFS: fstest.MapFS{
		"alpha/SKILL.md": {Data: []byte("alpha v2\n")},
		ManifestFileName: {Data: []byte(data)},
	}, fail: "festival-app"}

	inv, err := Classify(t.Context(), fsys, shipped, nil)
	if err != nil {
		t.Fatalf("Classify() error = %v, want an unreadable plugin directory to be no error", err)
	}
	next := inv.NextManifest("v2", nil)
	if next.Providers["festival-app"].Plugin != "camp-app" || next.Bundles["festival-app"]["SKILL.md"] != "ddd" {
		t.Fatalf("NextManifest() = %+v, want the unreadable plugin bundle's record kept", next)
	}
	if inv.ManifestStale() {
		t.Fatal("ManifestStale() = true, want an unreadable plugin directory to change nothing")
	}
}

func TestReadManifest_MalformedProvidersCountAsNone(t *testing.T) {
	base := manifestYAML(map[string]map[string]string{"alpha": {"SKILL.md": "aaa"}})
	for name, providers := range map[string]string{
		"a string":                  "providers: oops\n",
		"a list":                    "providers:\n  - festival-app\n",
		"an entry that is a list":   "providers:\n  festival-app: [1, 2]\n",
		"a field of the wrong type": "providers:\n  festival-app:\n    declined: maybe\n",
	} {
		m, err := ReadManifest(fstest.MapFS{ManifestFileName: {Data: []byte(base + providers)}})
		if err != nil {
			t.Fatalf("%s: ReadManifest() error = %v, want malformed providers ignored", name, err)
		}
		if m.Providers != nil || m.Bundles["alpha"]["SKILL.md"] != "aaa" {
			t.Fatalf("%s: manifest = %+v, want camp records and no providers", name, m)
		}
	}
}

// A manifest that is not a mapping at all is still refused with the same
// bytes camp printed before plugin records existed.
func TestReadManifest_NotAMappingErrorIsUnchanged(t *testing.T) {
	cases := map[string]string{
		"foo\n": "parse .campaign/skills/.shipped.yaml; remove it to rebuild the record: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!str `foo` into skills.Manifest",
		"- a\n": "parse .campaign/skills/.shipped.yaml; remove it to rebuild the record: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!seq into skills.Manifest",
	}
	for data, want := range cases {
		_, err := ReadManifest(fstest.MapFS{ManifestFileName: {Data: []byte(data)}})
		if err == nil || err.Error() != want {
			t.Errorf("ReadManifest(%q) error = %q, want %q", data, err, want)
		}
	}
}

// One malformed entry is skipped; the declined record beside it is kept, so
// an automatic request still leaves the bundle declined.
func TestReadManifest_MalformedSiblingKeepsADecline(t *testing.T) {
	data := manifestYAML(map[string]map[string]string{"alpha": {"SKILL.md": HashContent([]byte("alpha v2\n"))}}) +
		"providers:\n  other: [1]\n  festival-app:\n    plugin: camp-app\n    declined: true\n  broken:\n    declined: maybe\n"
	fsys := fstest.MapFS{"alpha/SKILL.md": {Data: []byte("alpha v2\n")}, ManifestFileName: {Data: []byte(data)}}
	m, err := ReadManifest(fsys)
	if err != nil {
		t.Fatal(err)
	}
	want := ProviderRecords{"festival-app": {Plugin: CampAppPlugin, Declined: true}}
	if !maps.Equal(m.Providers, want) {
		t.Fatalf("Providers = %+v, want only the valid decline", m.Providers)
	}
	offer := offered("0.4.0", newSkill)
	inv, err := Classify(t.Context(), fsys, append([]ShippedFile{{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")}}, offer.Files...), nil)
	if err != nil {
		t.Fatal(err)
	}
	inv.applyPluginRules([]PluginBundle{offer}, true)
	if f := inv.Files[1]; f.State != ShippedDeclined || actionFor(f.State, false).Writes() {
		t.Fatalf("festival-app state = %s, want declined and left alone", f.State)
	}
	if next := inv.NextManifest("v2", nil); !next.Providers["festival-app"].Declined {
		t.Fatalf("the decline did not survive: %+v", next.Providers)
	}
}
