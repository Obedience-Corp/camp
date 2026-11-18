package skills

// pluginRun is a plugin bundle this run classified, and how it was asked for.
type pluginRun struct {
	plugin  string
	version string
	// keepDeclined is set when an automatic request met a declined bundle.
	keepDeclined bool
	// incomparable is set when the offered or recorded version cannot be
	// compared, so an existing bundle that differs is preserved.
	incomparable bool
}

// PluginNote is how the plugin rules decided one plugin bundle.
type PluginNote struct {
	Plugin string
	// Offered is the version this machine's plugin offers.
	Offered string
	// Recorded is the version recorded before this run, if any.
	Recorded string
	// Incomparable is set when Offered or Recorded cannot be compared.
	Incomparable bool
}

// applyPluginRules decides each plugin bundle beyond its file states: an
// automatic request leaves a declined bundle alone, an older plugin than the
// one that wrote the bundle leaves it alone (ahead), and a version that cannot
// be compared turns an outdated bundle into a preserved one. Matching bytes
// always win.
func (inv *Inventory) applyPluginRules(bundles []PluginBundle, auto bool) map[string]PluginNote {
	inv.plugins = make(map[string]pluginRun, len(bundles))
	notes := make(map[string]PluginNote, len(bundles))
	for _, b := range bundles {
		rec, recorded := inv.Manifest.providers()[b.Name]
		run := pluginRun{plugin: b.Plugin, version: b.Version}
		note := PluginNote{Plugin: b.Plugin, Offered: b.Version}
		if recorded {
			note.Recorded = rec.Version
		}
		switch {
		case recorded && rec.Declined && auto:
			run.keepDeclined = true
			inv.setBundleState(b.Name, func(FileStatus) ShippedState { return ShippedDeclined })
		case recorded && !rec.Declined && inv.bundleDiffers(b.Name) && !inv.bundleMissing(b.Name):
			run.incomparable = inv.applyVersionRule(b.Name, b.Version, rec.Version)
		}
		note.Incomparable = run.incomparable
		inv.plugins[b.Name] = run
		notes[b.Name] = note
	}
	return notes
}

// applyVersionRule marks a differing bundle ahead when the offered version is
// older than the recorded one, and reports whether the two cannot be compared,
// in which case outdated files become unknown and are preserved. Versions
// that order as equal but are spelled differently cannot be compared.
func (inv *Inventory) applyVersionRule(slug, offered, recorded string) bool {
	if recorded == "" {
		return false
	}
	o, okOffered := parsePluginVersion(offered)
	r, okRecorded := parsePluginVersion(recorded)
	switch {
	case !okOffered || !okRecorded || (o.compare(r) == 0 && offered != recorded):
		// Two builds that order the same but are not the same build (other
		// commits at one count, or a dirty tree) cannot say which is newer.
		// Only the identical version with other content is a local rebuild.
		inv.setBundleState(slug, func(f FileStatus) ShippedState {
			if f.State == ShippedOutdated {
				return ShippedUnknown
			}
			return f.State
		})
		return true
	case o.compare(r) < 0:
		inv.setBundleState(slug, func(f FileStatus) ShippedState {
			if f.State == ShippedCurrent || f.Problem != "" {
				return f.State
			}
			return ShippedAhead
		})
	}
	return false
}

func (inv *Inventory) setBundleState(slug string, state func(FileStatus) ShippedState) {
	for i := range inv.Files {
		if inv.Files[i].Bundle == slug {
			inv.Files[i].State = state(inv.Files[i])
		}
	}
}

func (inv *Inventory) bundleDiffers(slug string) bool {
	for _, f := range inv.Files {
		if f.Bundle == slug && f.State != ShippedCurrent {
			return true
		}
	}
	return false
}

func (inv *Inventory) bundleMissing(slug string) bool {
	for _, f := range inv.Files {
		if f.Bundle == slug && f.State != ShippedMissing {
			return false
		}
	}
	return true
}

// pluginRecord is the provider record a plugin bundle classified in this run
// leaves in the manifest. A write records the plugin version that wrote it. A
// bundle already current keeps its record, so a camp shared between machines
// is not rewritten. Naming a declined bundle without --auto clears the decline.
func (inv *Inventory) pluginRecord(slug string, run pluginRun, written map[string]bool) (ProviderRecord, bool) {
	rec, recorded := inv.Manifest.providers()[slug]
	if run.keepDeclined {
		return rec, true
	}
	wrote, allCurrent := false, true
	for _, f := range inv.Files {
		if f.Bundle != slug {
			continue
		}
		wrote = wrote || written[f.RelPath()]
		allCurrent = allCurrent && (f.State == ShippedCurrent || written[f.RelPath()])
	}
	switch {
	case wrote, allCurrent && (!recorded || rec.Declined):
		return ProviderRecord{Plugin: run.plugin, Version: run.version}, true
	case recorded:
		rec.Declined = false
		return rec, true
	default:
		return ProviderRecord{}, false
	}
}
