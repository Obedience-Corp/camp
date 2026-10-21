package workitem

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

func emitDoctorReport(cmd *cobra.Command, findings []docFinding, fix bool) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	return emitDocHuman(cmd.OutOrStdout(), findings, ui.TermColumns(), verbose, fix)
}

// Keep presentation separate from the versioned findings and repair decisions.
func emitDocHuman(w io.Writer, findings []docFinding, width int, verbose, fix bool) error {
	var out strings.Builder
	line := func(indent, text string) {
		if width > 0 {
			text = ansi.Wrap(text, max(1, width-len(indent)), "")
		}
		for _, part := range strings.Split(text, "\n") {
			out.WriteString(indent + part + "\n")
		}
	}
	errors, warnings, infos, repairable := 0, 0, 0, 0
	for _, f := range findings {
		switch f.Severity {
		case docSeverityError:
			errors++
		case docSeverityWarning:
			warnings++
		case docSeverityInfo:
			infos++
		}
		if f.AutoFixable {
			repairable++
		}
	}
	line("", ui.Header("Workitem health"))
	line("", fmt.Sprintf("%s, %s, %s", doctorCount(errors, "error"), doctorCount(warnings, "warning"), doctorCount(infos, "informational summary")))
	if len(findings) == 0 {
		line("", ui.Success("No issues found."))
	}
	sections := []struct {
		title   string
		tone    ui.StatusTone
		accepts func(docFinding) bool
	}{
		{"Errors", ui.StatusError, func(f docFinding) bool { return f.Severity == docSeverityError }},
		{"Needs review", ui.StatusWarning, func(f docFinding) bool { return f.Severity == docSeverityWarning }},
		{"Information", ui.StatusInfo, func(f docFinding) bool { return f.Severity == docSeverityInfo && f.Code != codeWorktreeGone }},
		{"No action needed", ui.StatusMuted, func(f docFinding) bool { return f.Code == codeWorktreeGone && f.Severity == docSeverityInfo }},
	}
	for _, section := range sections {
		var selected []docFinding
		for _, f := range findings {
			if section.accepts(f) {
				selected = append(selected, f)
			}
		}
		if len(selected) == 0 {
			continue
		}
		line("", "")
		line("", ui.Accent(section.title))
		for i, group := range doctorGroups(selected) {
			if i > 0 {
				line("", "")
			}
			f := group[0]
			title, details, hint := doctorFindingText(group)
			line("  ", ui.ChecklistMark(section.tone)+ui.BoldText(title))
			for _, detail := range details {
				line("    ", detail)
			}
			if hint != "" {
				line("    ", ui.Dim(hint))
			}
			if verbose {
				for _, member := range group {
					line("    ", ui.Dim(fmt.Sprintf("[%s] %s %s", member.Severity, member.Code, member.Target)))
					// Preserve the original diagnostic and exact per-link command for support.
					if member.Message != title {
						line("    ", ui.Dim(member.Message))
					}
					if member.FixHint != "" && member.FixHint != hint {
						line("    ", ui.Dim(member.FixHint))
					}
				}
			} else if f.AutoFixable {
				line("    ", ui.Info("Automatic repair available."))
			}
		}
	}
	if len(findings) > 0 {
		line("", "")
		if repairable > 0 {
			if fix {
				line("", fmt.Sprintf("%s still pending. See the repair messages above.", doctorCount(repairable, "automatic repair")))
			} else {
				line("", fmt.Sprintf("%s available: camp workitem doctor --fix", doctorCount(repairable, "automatic repair")))
			}
		} else if fix {
			line("", "No automatic repairs available for the remaining findings.")
		} else if errors+warnings > 0 {
			line("", "These findings need review; --fix cannot resolve them automatically.")
		}
		if !verbose {
			line("", ui.Dim("Details: camp workitem doctor --verbose"))
		}
	}
	_, err := io.WriteString(w, out.String())
	return err
}

func doctorCount(n int, noun string) string {
	if n != 1 {
		if noun == "informational summary" {
			noun = "informational summaries"
		} else {
			noun += "s"
		}
	}
	return fmt.Sprintf("%d %s", n, noun)
}

// Only coalesce unavailable worktrees sharing the same parent project. Keep
// every path visible, and every diagnostic available with --verbose or --json.
func doctorGroups(findings []docFinding) [][]docFinding {
	var groups [][]docFinding
	byProject := map[string]int{}
	for _, f := range findings {
		if f.Code == codeScopeNotLocal && f.scopeKind == links.ScopeWorktree && f.scopeProject != "" {
			if i, ok := byProject[f.scopeProject]; ok {
				groups[i] = append(groups[i], f)
				continue
			}
			byProject[f.scopeProject] = len(groups)
		}
		groups = append(groups, []docFinding{f})
	}
	return groups
}

func doctorFindingText(group []docFinding) (title string, details []string, hint string) {
	f := group[0]
	if f.Code == codeScopeNotLocal && f.scopeKind == links.ScopeWorktree {
		title = "Unavailable worktrees: " + doctorCount(len(group), "link")
		if f.scopeProject != "" {
			details = append(details, "Parent project also unavailable: "+f.scopeProject)
		}
		for _, member := range group {
			details = append(details, member.scopePath)
		}
		hint = "These may exist on another machine. Check whether the parent project was renamed or removed. Links are preserved."
		return
	}
	if f.Code == codeWorktreeGone && f.worktreeCount > 0 {
		title = doctorCount(f.worktreeCount, "worktree link") + " preserved"
		hint = "The worktrees are absent; their projects are still present. View saved links with camp workitem links."
		return
	}
	title = doctorFindingTitle(f.Code)
	if title == "" {
		title = f.Message
	} else {
		details = append(details, f.Message)
	}
	if f.Target != "" && !strings.HasPrefix(f.Target, "link:") {
		details = append(details, "Location: "+strings.TrimPrefix(f.Target, "workitem:"))
	}
	if strings.HasPrefix(f.Target, "link:") {
		details = append(details, "Link: "+strings.TrimPrefix(f.Target, "link:"))
	}
	hint = f.FixHint
	return
}

func doctorFindingTitle(code string) string {
	switch code {
	case codeProjectNotFound:
		return "Referenced project is missing"
	case codeProjectUnvalidatable:
		return "Project reference could not be checked"
	case codeRegistryParseError:
		return "Saved links could not be read"
	case codeBrokenLink:
		return "Linked workitem is missing"
	case codeBrokenScope:
		return "Linked location is missing"
	case codeOutOfBounds:
		return "Linked location is outside this camp"
	case codeScopeUnvalidatable:
		return "Linked location could not be checked"
	case codeScopeNotLocal:
		return "Linked location is unavailable on this machine"
	case codeWorkitemShelved:
		return "Link points to a shelved workitem"
	case codeDuplicatePrimary:
		return "More than one primary link"
	case codeSchemaViolation:
		return "Invalid link record"
	case codeMissingRefField:
		return "Workitem reference is missing"
	case codeWorkitemScanFailed:
		return "Workitems could not be scanned"
	case codeWorktreeProjectUnknown:
		return "Worktree project needs recording"
	case codeDeprecatedRelatedProject:
		return "Project link needs updating"
	case codeUnstampedResident:
		return "Workitem metadata is missing"
	case codeResidentMissingHome:
		return "Workitem type directory is missing"
	default:
		return ""
	}
}
