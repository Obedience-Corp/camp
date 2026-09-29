package project

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	projectrename "github.com/Obedience-Corp/camp/internal/project/rename"
	"github.com/spf13/cobra"
)

type renameStep int

const (
	renamePick renameStep = iota
	renameName
	renameReview
	renameWorking
	renameDone
)

type renamePlanner func(context.Context, string, string, string, projectrename.Options) (*projectrename.PlanResult, error)

type renameApplier func(context.Context, string, string, *projectrename.PlanResult, string, projectRenameFlags) (*projectrename.Result, *projectRenameCommit, error)

type renamePlannedMsg struct {
	plan *projectrename.PlanResult
	err  error
}

type renameAppliedMsg struct {
	result *projectrename.Result
	commit *projectRenameCommit
	err    error
}

type renameModel struct {
	ctx        context.Context
	root       string
	campaignID string
	flags      projectRenameFlags

	projects  []projectsvc.Project
	visible   []projectsvc.Project
	cursor    int
	query     string
	filtering bool

	nameInput     textinput.Model
	remoteInput   textinput.Model
	remoteBackup  string
	editingRemote bool

	oldName   string
	newName   string
	presetOld bool

	plan     *projectrename.PlanResult
	result   *projectrename.Result
	commit   *projectRenameCommit
	planning bool

	step   renameStep
	errMsg string
	width  int
	height int

	quitting bool
	spin     spinner.Model

	planFn  renamePlanner
	applyFn renameApplier
}

func runProjectRenameTUI(cmd *cobra.Command, args []string, flags projectRenameFlags) error {
	ctx := cmd.Context()
	resolver := newProjectCampaignResolver(cmd.ErrOrStderr(), "camp project rename --campaign <name>")
	cfg, root, err := resolver.Resolve(ctx, flags.campaign, cmd.Flags().Changed("campaign"))
	if err != nil {
		return err
	}
	projects, err := projectsvc.List(ctx, root)
	if err != nil {
		return err
	}
	campaignID := ""
	if cfg != nil {
		campaignID = cfg.ID
	}
	model := newRenameModel(ctx, root, campaignID, projects, args, flags)
	prog := tea.NewProgram(model, tea.WithContext(ctx), tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return camperrors.Wrap(err, "running project rename review")
	}
	return nil
}

func newRenameModel(ctx context.Context, root, campaignID string, projects []projectsvc.Project, args []string, flags projectRenameFlags) renameModel {
	projects = slices.Clone(projects)
	slices.SortFunc(projects, func(a, b projectsvc.Project) int {
		return cmp.Compare(a.Name, b.Name)
	})
	name := newRenameInput("new-name")
	remote := newRenameInput("git@github.com:org/name.git")
	remote.SetValue(flags.remoteURL)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(renamePal.Accent)

	m := renameModel{
		ctx:         ctx,
		root:        root,
		campaignID:  campaignID,
		flags:       flags,
		projects:    projects,
		nameInput:   name,
		remoteInput: remote,
		spin:        s,
		planFn:      projectrename.Plan,
		applyFn:     finishProjectRename,
	}
	// The review is already waiting on this confirmation.
	m.flags.synchronous = true
	if len(args) > 0 {
		m.oldName = args[0]
		m.presetOld = true
		m.cursor = renameProjectIndex(projects, args[0])
	}
	if len(args) > 1 {
		m.newName = args[1]
		m.nameInput.SetValue(args[1])
	}
	switch {
	case m.oldName != "" && m.newName != "":
		m.step = renameReview
		m.planning = true
	case m.oldName != "":
		m.step = renameName
		m.nameInput.Focus()
	default:
		m.step = renamePick
	}
	m.rebuildVisible()
	return m
}

func newRenameInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 160
	ti.Width = 48
	ti.Prompt = "› "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(renamePal.Accent).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(renamePal.TextPrimary)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(renamePal.TextMuted)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(renamePal.Accent)
	return ti
}

func renameProjectIndex(projects []projectsvc.Project, name string) int {
	i := slices.IndexFunc(projects, func(p projectsvc.Project) bool { return p.Name == name })
	if i < 0 {
		return 0
	}
	return i
}

func (m renameModel) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink}
	if m.step == renameName {
		cmds = append(cmds, m.nameInput.Focus())
	}
	if m.planning {
		cmds = append(cmds, m.planCmd())
	}
	return tea.Batch(cmds...)
}

func (m renameModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.fitInputs()
		return m, nil
	case spinner.TickMsg:
		if m.step != renameWorking {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case renamePlannedMsg:
		return m.planned(msg)
	case renameAppliedMsg:
		return m.applied(msg)
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func (m renameModel) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" && m.step != renameWorking {
		m.quitting = true
		return m, tea.Quit
	}
	switch m.step {
	case renamePick:
		return m.onPickKey(msg)
	case renameName:
		return m.onNameKey(msg)
	case renameReview:
		return m.onReviewKey(msg)
	case renameDone:
		switch msg.String() {
		case "enter", "esc", "q":
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m renameModel) onPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filtering = false
			m.query = ""
			m.rebuildVisible()
			return m, nil
		case "enter":
			m.filtering = false
			return m.choose()
		case "backspace":
			if m.query == "" {
				m.filtering = false
				return m, nil
			}
			runes := []rune(m.query)
			m.query = string(runes[:len(runes)-1])
			m.rebuildVisible()
			return m, nil
		case "up":
			return m.move(-1), nil
		case "down":
			return m.move(1), nil
		default:
			if isBrowsePrintable(msg) {
				m.query += msg.String()
				m.rebuildVisible()
			}
			return m, nil
		}
	}
	switch msg.String() {
	case "q", "esc":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		return m.move(-1), nil
	case "down", "j":
		return m.move(1), nil
	case "/":
		m.filtering = true
		m.query = ""
		return m, nil
	case "enter":
		return m.choose()
	}
	return m, nil
}

func (m renameModel) choose() (tea.Model, tea.Cmd) {
	p, ok := m.selected()
	if !ok {
		return m, nil
	}
	m.oldName = p.Name
	m.errMsg = ""
	m.step = renameName
	return m, m.nameInput.Focus()
}

func (m renameModel) onNameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.errMsg = ""
		if m.presetOld {
			m.quitting = true
			return m, tea.Quit
		}
		m.step = renamePick
		return m, nil
	case "enter":
		return m.submitName()
	default:
		var cmd tea.Cmd
		m.nameInput, cmd = m.nameInput.Update(msg)
		return m, cmd
	}
}

func (m renameModel) submitName() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.nameInput.Value())
	if name == "" {
		m.errMsg = "new name is required"
		return m, nil
	}
	if name == m.oldName {
		m.errMsg = "new name must differ from the current name"
		return m, nil
	}
	m.newName = name
	m.errMsg = ""
	m.plan = nil
	m.planning = true
	m.step = renameReview
	return m, m.planCmd()
}

func (m renameModel) onReviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.editingRemote {
		switch msg.String() {
		case "esc":
			m.editingRemote = false
			m.remoteInput.SetValue(m.remoteBackup)
			return m, nil
		case "enter":
			m.editingRemote = false
			m.errMsg = ""
			m.plan = nil
			m.planning = true
			return m, m.planCmd()
		default:
			var cmd tea.Cmd
			m.remoteInput, cmd = m.remoteInput.Update(msg)
			return m, cmd
		}
	}
	switch msg.String() {
	case "q":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.errMsg = ""
		m.planning = false
		m.step = renameName
		return m, m.nameInput.Focus()
	case "u":
		if m.planning {
			return m, nil
		}
		m.editingRemote = true
		m.remoteBackup = m.remoteInput.Value()
		return m, m.remoteInput.Focus()
	case "enter":
		return m.confirm()
	}
	return m, nil
}

func (m renameModel) confirm() (tea.Model, tea.Cmd) {
	if m.planning || m.plan == nil {
		return m, nil
	}
	if m.flags.dryRun {
		m.step = renameDone
		return m, nil
	}
	m.step = renameWorking
	m.errMsg = ""
	return m, tea.Batch(m.spin.Tick, m.applyCmd())
}

func (m renameModel) planned(msg renamePlannedMsg) (tea.Model, tea.Cmd) {
	m.planning = false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		m.plan = nil
		m.step = renameName
		return m, m.nameInput.Focus()
	}
	m.plan = msg.plan
	m.errMsg = ""
	m.step = renameReview
	return m, nil
}

func (m renameModel) applied(msg renameAppliedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.step = renameReview
		m.errMsg = msg.err.Error()
		if msg.result != nil && msg.result.RolledBack {
			m.errMsg += " (changes rolled back)"
		}
		return m, nil
	}
	m.result = msg.result
	m.commit = msg.commit
	if msg.result != nil && msg.result.Plan != nil {
		m.plan = msg.result.Plan
	}
	m.step = renameDone
	m.errMsg = ""
	return m, nil
}

func (m renameModel) planCmd() tea.Cmd {
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	root, oldName, newName, remote := m.root, m.oldName, m.newName, m.remoteURL()
	flags := m.flags
	fn := m.planFn
	return func() tea.Msg {
		plan, err := fn(ctx, root, oldName, newName, projectrename.Options{
			RemoteURL: remote, VerifyRemote: !flags.noVerify,
		})
		return renamePlannedMsg{plan: plan, err: err}
	}
}

func (m renameModel) applyCmd() tea.Cmd {
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	root, campaignID, remote := m.root, m.campaignID, m.remoteURL()
	plan, flags, fn := m.plan, m.flags, m.applyFn
	return func() tea.Msg {
		result, commit, err := fn(ctx, campaignID, root, plan, remote, flags)
		return renameAppliedMsg{result: result, commit: commit, err: err}
	}
}

func (m renameModel) remoteURL() string {
	return strings.TrimSpace(m.remoteInput.Value())
}

func (m *renameModel) fitInputs() {
	w := m.width - 8
	if w < 16 {
		w = 16
	}
	if w > 56 {
		w = 56
	}
	m.nameInput.Width = w
	m.remoteInput.Width = w
}

func (m *renameModel) rebuildVisible() {
	q := strings.ToLower(strings.TrimSpace(m.query))
	if q == "" {
		m.visible = slices.Clone(m.projects)
	} else {
		matched := make([]projectsvc.Project, 0, len(m.projects))
		for _, p := range m.projects {
			if strings.Contains(strings.ToLower(p.Name), q) || strings.Contains(strings.ToLower(p.Path), q) {
				matched = append(matched, p)
			}
		}
		m.visible = matched
	}
	if len(m.visible) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
}

func (m renameModel) move(delta int) renameModel {
	n := len(m.visible)
	if n == 0 {
		return m
	}
	m.cursor = (m.cursor + delta + n) % n
	return m
}

func (m renameModel) selected() (projectsvc.Project, bool) {
	if len(m.visible) == 0 || m.cursor < 0 || m.cursor >= len(m.visible) {
		return projectsvc.Project{}, false
	}
	return m.visible[m.cursor], true
}

func (m renameModel) projectByName(name string) (projectsvc.Project, bool) {
	i := slices.IndexFunc(m.projects, func(p projectsvc.Project) bool { return p.Name == name })
	if i < 0 {
		return projectsvc.Project{}, false
	}
	return m.projects[i], true
}

func (m renameModel) shownPlan() *projectrename.PlanResult {
	if m.result != nil && m.result.Plan != nil {
		return m.result.Plan
	}
	return m.plan
}

func renameKindLabel(kind projectrename.Kind) string {
	switch kind {
	case projectrename.KindSubmodule:
		return "submodule"
	case projectrename.KindLinked:
		return "linked workspace"
	case projectrename.KindCampaignDir:
		return "camp directory"
	default:
		if kind == "" {
			return "project"
		}
		return string(kind)
	}
}

func renameWorktreeSummary(changes []projectrename.WorktreeChange) string {
	if len(changes) == 0 {
		return "none"
	}
	moved, external := 0, 0
	for _, change := range changes {
		if change.Moved {
			moved++
		}
		if change.External {
			external++
		}
	}
	return fmt.Sprintf("%d moved · %d kept outside", moved, external)
}

func renameMetadataSummary(changes []projectrename.MetadataChange) string {
	records := 0
	for _, change := range changes {
		records += change.Records
	}
	return fmt.Sprintf("%d stores · %d records", len(changes), records)
}
