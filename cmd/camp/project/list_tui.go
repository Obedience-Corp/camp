package project

import (
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/shell"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// stdoutIsTTY is overridden in tests so dispatch does not depend on the
// runner's terminal.
var stdoutIsTTY = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

type projOverlay int

const (
	projOverlayNone projOverlay = iota
	projOverlaySearch
)

type projGroupBy int

const (
	groupByType projGroupBy = iota
	groupBySource
	groupByNone
)

func (g projGroupBy) label() string {
	switch g {
	case groupBySource:
		return "source"
	case groupByNone:
		return "flat"
	default:
		return "type"
	}
}

func (g projGroupBy) next() projGroupBy {
	switch g {
	case groupByType:
		return groupBySource
	case groupBySource:
		return groupByNone
	default:
		return groupByType
	}
}

// projectListItem is one browser row. AbsPath is the on-disk directory used
// for go/copy; RelPath is the campaign-relative display path.
type projectListItem struct {
	Name       string
	RelPath    string
	AbsPath    string
	Type       string
	Source     string
	URL        string
	LinkedPath string
}

type projListModel struct {
	all     []projectListItem
	visible []projectListItem
	cursor  int
	groupBy projGroupBy
	query   string

	overlay projOverlay
	input   textinput.Model

	status    string
	statusErr bool

	// gotoEnabled mirrors camp list: "go" only cds when the shell wrapper
	// passed --path-output. Without it, enter/g explains how to enable it
	// rather than silently doing nothing.
	gotoEnabled bool
	gotoPath    string

	width    int
	height   int
	quitting bool
}

// projectListTUIRequested decides whether bare `camp project list` opens the
// browser. --json/--count and a non-table --format never do; -i forces it;
// otherwise an interactive terminal opens it.
func projectListTUIRequested(cmd *cobra.Command, isTTY bool) bool {
	if projectListJSON || projectListCount {
		return false
	}
	if format, _ := cmd.Flags().GetString("format"); format != "table" {
		return false
	}
	if interactive, _ := cmd.Flags().GetBool("interactive"); interactive {
		return true
	}
	return isTTY
}

func runProjectListTUI(cmd *cobra.Command, root string, projects []projectsvc.Project) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return projectsvc.FormatProjects(os.Stdout, projects, projectsvc.FormatTable)
	}
	pathOutput, _ := cmd.Flags().GetString("path-output")
	model := newProjListModel(root, projects)
	model.gotoEnabled = pathOutput != ""

	prog := tea.NewProgram(model, tea.WithContext(cmd.Context()), tea.WithAltScreen())
	final, err := prog.Run()
	if err != nil {
		return camperrors.Wrap(err, "running project browser")
	}
	return writeGotoSelection(final, pathOutput)
}

func writeGotoSelection(final tea.Model, pathOutput string) error {
	m, ok := final.(projListModel)
	if !ok || pathOutput == "" || m.gotoPath == "" {
		return nil
	}
	return os.WriteFile(pathOutput, []byte(m.gotoPath), 0o600)
}

func newProjListModel(root string, projects []projectsvc.Project) projListModel {
	all := make([]projectListItem, 0, len(projects))
	for _, p := range projects {
		all = append(all, projectListItem{
			Name:       p.Name,
			RelPath:    p.Path,
			AbsPath:    projectsvc.ResolveProjectPath(root, p),
			Type:       p.Type,
			Source:     normalizeSource(p.Source),
			URL:        p.URL,
			LinkedPath: p.LinkedPath,
		})
	}
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "filter"
	m := projListModel{all: all, groupBy: groupByType, input: ti}
	m.rebuildVisible()
	return m
}

func normalizeSource(source string) string {
	switch source {
	case projectsvc.SourceLinked, projectsvc.SourceLinkedNonGit:
		return projectsvc.SourceLinked
	case projectsvc.SourceCampaign:
		return projectsvc.SourceCampaign
	default:
		return projectsvc.SourceSubmodule
	}
}

func (m *projListModel) rebuildVisible() {
	q := strings.ToLower(strings.TrimSpace(m.query))
	out := make([]projectListItem, 0, len(m.all))
	for _, e := range m.all {
		if q != "" && !itemMatches(e, q) {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return lessProjectItem(out[i], out[j], m.groupBy)
	})
	m.visible = out
	m.cursor = ui.ClampIdx(m.cursor, len(m.visible))
}

func itemMatches(e projectListItem, q string) bool {
	fields := []string{
		e.Name, e.RelPath, e.AbsPath, e.Type, typeLabel(e.Type),
		e.Source, sourceLabel(e.Source), e.URL, e.LinkedPath,
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), q) {
			return true
		}
	}
	return false
}

func lessProjectItem(a, b projectListItem, groupBy projGroupBy) bool {
	switch groupBy {
	case groupByType:
		if ra, rb := typeRank(a.Type), typeRank(b.Type); ra != rb {
			return ra < rb
		}
	case groupBySource:
		if ra, rb := sourceRank(a.Source), sourceRank(b.Source); ra != rb {
			return ra < rb
		}
	}
	return a.Name < b.Name
}

func typeRank(t string) int {
	switch t {
	case projectsvc.TypeGo:
		return 0
	case projectsvc.TypeRust:
		return 1
	case projectsvc.TypeTypeScript, "javascript":
		return 2
	case projectsvc.TypePython:
		return 3
	default:
		return 4
	}
}

func sourceRank(s string) int {
	switch s {
	case projectsvc.SourceSubmodule:
		return 0
	case projectsvc.SourceCampaign:
		return 1
	case projectsvc.SourceLinked:
		return 2
	default:
		return 3
	}
}

func typeLabel(t string) string {
	if t == "" {
		return "other"
	}
	return t
}

func sourceLabel(s string) string {
	switch s {
	case projectsvc.SourceLinked:
		return "linked"
	case projectsvc.SourceCampaign:
		return "campaign"
	default:
		return "submodule"
	}
}

func groupKey(groupBy projGroupBy, e projectListItem) string {
	switch groupBy {
	case groupByType:
		return typeLabel(e.Type)
	case groupBySource:
		return sourceLabel(e.Source)
	default:
		return ""
	}
}

func (m projListModel) Init() tea.Cmd { return nil }

func (m *projListModel) copyPath() error {
	return ui.WriteClipboard(m.visible[m.cursor].AbsPath)
}

func (m *projListModel) setStatus(s string, isErr bool) {
	m.status = s
	m.statusErr = isErr
}

func (m projListModel) goNeedsShellHint() string {
	return "go needs shell integration: run " + shell.InitHint()
}

func (m projListModel) distinctGroups() int {
	if m.groupBy == groupByNone {
		return 0
	}
	seen := map[string]bool{}
	for _, e := range m.visible {
		seen[groupKey(m.groupBy, e)] = true
	}
	return len(seen)
}
