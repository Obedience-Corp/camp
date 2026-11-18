package skills

import (
	"cmp"
	"fmt"
	"io"

	intskills "github.com/Obedience-Corp/camp/internal/skills"
)

// updateOutput is everything one `camp skills update` run reports.
type updateOutput struct {
	res   *intskills.UpdateResult
	plan  pluginPlan
	links []intskills.BundleLink
	link  bool
}

func (o updateOutput) print(out, errOut io.Writer, asJSON bool) error {
	if r := o.plan.report; r != nil && r.Status != intskills.ProviderOK {
		if _, err := fmt.Fprintln(errOut, r.SkipLine()); err != nil {
			return err
		}
	}
	if asJSON {
		return o.printJSON(out)
	}
	return writeLines(out, o.lines())
}

func writeLines(out io.Writer, lines []string) error {
	for _, line := range lines {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}

func (o updateOutput) lines() []string {
	res := o.res
	var lines []string
	current, added := 0, false
	said := make(map[string]bool)
	for _, f := range res.Files {
		note := res.Plugins[f.Bundle]
		switch {
		case f.State == intskills.ShippedDeclined, f.State == intskills.ShippedAhead:
			if !said[f.Bundle] {
				lines = append(lines, leftAloneLine(f.Bundle, f.State, note))
				said[f.Bundle] = true
			}
		case f.Action == intskills.ActionKeep:
			current++
		case f.Problem != "":
			lines = append(lines, fmt.Sprintf("left %s alone: %s; camp does not replace it", f.Bundle, f.Problem))
		case f.State == intskills.ShippedSymlink:
			lines = append(lines, fmt.Sprintf("left %s alone: it is a symlink, and camp never replaces symlinked skill files", f.RelPath()))
		case f.Action == intskills.ActionPreserve:
			lines = append(lines, fmt.Sprintf("preserved %s (%s); run 'camp skills update --force %s' to replace it",
				f.RelPath(), fileNote(f.FileStatus, note), f.Bundle))
		case res.DryRun:
			lines = append(lines, fmt.Sprintf("would %s %s (%s)", f.Action, f.RelPath(), fileNote(f.FileStatus, note)))
		case f.Written:
			lines = append(lines, fmt.Sprintf("%s %s (%s)", pastTense(f.Action), f.RelPath(), fileNote(f.FileStatus, note)))
			added = added || f.Action == intskills.ActionAdd
		}
	}
	for _, u := range o.plan.unresolved {
		lines = append(lines, unresolvedLine(u, o.plan.report))
	}
	lines = append(lines, o.undoLines()...)
	lines = append(lines, fmt.Sprintf("%d file(s) already current", current))
	switch {
	case res.DryRun:
		lines = append(lines, "dry run: nothing written")
	case res.ManifestWritten:
		lines = append(lines, "recorded .campaign/skills/"+intskills.ManifestFileName)
	}
	if added && !o.link {
		lines = append(lines, "run 'camp skills link' to project added bundles into tool skill directories")
	}
	for _, l := range o.links {
		lines = append(lines, linkLine(l))
	}
	return lines
}

func (o updateOutput) undoLines() []string {
	var lines []string
	for _, u := range o.res.Undo {
		if u.BackupDir != "" {
			lines = append(lines, fmt.Sprintf("backed up replaced %s files to %s", u.Bundle, u.BackupDir))
		}
		lines = append(lines, fmt.Sprintf("undo %s: %s (camp skills status then shows %s as modified)", u.Bundle, u.Command, u.Bundle))
	}
	for _, b := range o.bundleRows() {
		if b.Action == "added" && b.Source != sourceCamp && b.Undo != "" {
			lines = append(lines, fmt.Sprintf("undo %s: %s", b.Slug, b.Undo))
		}
	}
	return lines
}

func leftAloneLine(slug string, state intskills.ShippedState, note intskills.PluginNote) string {
	if state == intskills.ShippedDeclined {
		return fmt.Sprintf("left %s alone: declined in this camp; run 'camp skills update %s' to add it", slug, slug)
	}
	return fmt.Sprintf("kept .campaign/skills/%s (written by %s; this machine has %s)",
		slug, pluginAt(note.Plugin, note.Recorded), displayVersion(note.Offered))
}

// pluginAt names a plugin and, when known, its version.
func pluginAt(plugin, version string) string {
	if version == "" {
		return plugin
	}
	return plugin + " " + version
}

// unresolvedLine reports a recorded plugin bundle camp-app could not serve.
// For a declined one it never repeats the command just run as the way back:
// adding it back needs camp-app, so it says why camp-app could not answer.
func unresolvedLine(u unresolvedBundle, report *intskills.ProviderReport) string {
	plugin := cmp.Or(u.plugin, intskills.CampAppPlugin)
	reason := plugin + " could not be asked"
	switch {
	case u.notOffered:
		reason = plugin + " no longer offers it"
	case report != nil && report.Detail != "":
		reason = report.Detail
	}
	if u.state == intskills.ShippedDeclined {
		return fmt.Sprintf("left %s alone: declined in this camp; adding it back needs %s, and %s", u.slug, plugin, reason)
	}
	if u.notOffered {
		return fmt.Sprintf("left %s alone: %s (written by %s)", u.slug, reason, pluginAt(plugin, u.version))
	}
	return fmt.Sprintf("left %s alone: %s cannot refresh it on this machine (written by %s)", u.slug, plugin, pluginAt(plugin, u.version))
}

// fileNote is the existing state note, plus the source of a plugin bundle.
func fileNote(f intskills.FileStatus, note intskills.PluginNote) string {
	if f.Plugin == "" {
		return stateNote(f)
	}
	switch {
	case note.Incomparable && f.State == intskills.ShippedUnknown:
		return fmt.Sprintf("differs; %s %s cannot be compared with the recorded %s", f.Plugin, displayVersion(note.Offered), displayVersion(note.Recorded))
	case f.State == intskills.ShippedOutdated && note.Recorded != "" && note.Offered != "":
		return fmt.Sprintf("%s; %s %s -> %s", stateNote(f), f.Plugin, note.Recorded, note.Offered)
	default:
		return fmt.Sprintf("%s; from %s", stateNote(f), pluginAt(f.Plugin, note.Offered))
	}
}

func displayVersion(v string) string {
	if v == "" {
		return "(none)"
	}
	return v
}

func linkLine(l intskills.BundleLink) string {
	switch l.Result {
	case intskills.LinkLinked:
		return "linked " + l.Path
	case intskills.LinkAlreadyLinked:
		return "already linked " + l.Path
	case intskills.LinkLeftToObey:
		return "left " + l.Path + " to Obey, which links .campaign/skills there itself"
	case intskills.LinkNoFolder:
		return "no " + l.Path + " folder; camp does not create one for a single skill"
	case intskills.LinkConflict:
		return "left " + l.Path + " alone: something that is not camp's link is already there"
	default:
		return "left " + l.Path + " alone: camp has not linked skills there"
	}
}

func pastTense(a intskills.FileAction) string {
	switch a {
	case intskills.ActionAdd:
		return "added"
	case intskills.ActionUpdate:
		return "updated"
	default:
		return "replaced"
	}
}

func stateNote(f intskills.FileStatus) string {
	switch f.State {
	case intskills.ShippedMissing:
		return "missing"
	case intskills.ShippedOutdated:
		if f.GitEvidence {
			return "unchanged since camp wrote it, per git history"
		}
		return "outdated"
	case intskills.ShippedModified:
		return "changed locally"
	default:
		return "differs, no record or git history of what camp shipped"
	}
}
