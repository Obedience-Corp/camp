package linked

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
)

// stdoutIsTTY reports whether stdout is an interactive terminal.
// Tests replace it so dispatch does not depend on the process stdout.
var stdoutIsTTY = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

type linkStep int

const (
	stepFolder linkStep = iota
	stepName
	stepCamp
	stepReview
	stepWork
	stepDone
)

type linkEntry struct {
	label string
	path  string
	kind  string // here, up, dir, place
	badge string
}

type linkCamp struct {
	ID   string
	Name string
	Path string
}

type linkCommitNote struct {
	Committed bool
	Message   string
}

type linkPlanFunc func(context.Context, string, string, string) (*projectsvc.LinkPlan, error)

type linkApplyFunc func(context.Context, linkCamp, *projectsvc.LinkPlan, bool) (*projectsvc.LinkResult, linkCommitNote, error)

type linkStatFunc func(string) (bool, error)

type linkOpen struct {
	ctx       context.Context
	browse    string
	offerHere bool
	chosen    string
	name      string
	nameSet   bool
	camp      linkCamp
	hasCamp   bool
	camps     []linkCamp
	forceCamp bool
	noCommit  bool
	home      string
	homes     []string
	listFn    func(string, bool) ([]linkEntry, error)
	statFn    linkStatFunc
	planFn    linkPlanFunc
	applyFn   linkApplyFunc
}

type linkPlannedMsg struct {
	plan *projectsvc.LinkPlan
	err  error
	gen  int
}

type linkAppliedMsg struct {
	result *projectsvc.LinkResult
	note   linkCommitNote
	err    error
}

type linkModel struct {
	ctx      context.Context
	noCommit bool
	homes    []string

	camps          []linkCamp
	campVisible    []linkCamp
	camp           linkCamp
	campBackup     linkCamp
	hasCamp        bool
	hadCamp        bool
	campPicked     bool
	forceCamp      bool
	campFromReview bool
	campCursor     int
	campQuery      string
	campFiltering  bool

	cwd         string
	anchor      string
	anchorOffer bool
	dirs        []linkEntry
	visible     []linkEntry
	cursor      int
	query       string
	showHidden  bool
	offerHere   bool
	preset      bool

	nameInput textinput.Model
	pathInput textinput.Model
	nameSet   bool

	chosenPath string
	chosenName string

	plan     *projectsvc.LinkPlan
	result   *projectsvc.LinkResult
	note     linkCommitNote
	planning bool
	planGen  int

	step   linkStep
	errMsg string
	width  int
	height int

	quitting bool
	spin     spinner.Model

	listFn  func(string, bool) ([]linkEntry, error)
	statFn  linkStatFunc
	planFn  linkPlanFunc
	applyFn linkApplyFunc
}

func runLinkTUI(cmd *cobra.Command, args []string, flags linkFlags, newResolver CampaignResolverFactory) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	open, err := prepareLinkOpen(ctx, cmd, args, flags, newResolver)
	if err != nil {
		return err
	}
	model := newLinkModel(open)
	prog := tea.NewProgram(model, tea.WithContext(ctx), tea.WithAltScreen())
	final, err := prog.Run()
	if err != nil {
		return camperrors.Wrap(err, "running project link")
	}
	linked, ok := final.(linkModel)
	if !ok || linked.step != stepDone || linked.result == nil {
		return nil
	}
	PrintResult(linked.result)
	if linked.note.Message != "" {
		fmt.Printf("  %s\n", linked.note.Message)
	}
	return nil
}

func prepareLinkOpen(ctx context.Context, cmd *cobra.Command, args []string, flags linkFlags, newResolver CampaignResolverFactory) (linkOpen, error) {
	detected, hasDetected := detectCurrentCamp(ctx)
	var selected linkCamp
	hasSelected := false
	switch {
	case flags.campaign != "":
		resolver := newResolver(cmd.ErrOrStderr(), "camp project link [path] --campaign <name>")
		cfg, root, err := resolver.Resolve(ctx, flags.campaign, true)
		if err != nil {
			return linkOpen{}, err
		}
		selected = campFromConfig(cfg, root)
		hasSelected = true
	case hasDetected && !flags.forceCamp:
		selected = detected
		hasSelected = true
	}

	camps, err := loadLinkCamps(ctx)
	if err != nil && !hasDetected && !hasSelected {
		return linkOpen{}, err
	}
	if hasDetected {
		camps = ensureLinkCamp(camps, detected)
	}
	if hasSelected {
		camps = ensureLinkCamp(camps, selected)
	}
	if flags.forceCamp && hasDetected {
		selected = detected
		hasSelected = true
		camps = ensureLinkCamp(camps, detected)
	}
	if !hasSelected && !flags.forceCamp && len(camps) == 1 {
		selected = camps[0]
		hasSelected = true
	}
	if !hasSelected && len(camps) == 0 {
		return linkOpen{}, camperrors.Wrap(camperrors.ErrNotInitialized, "no camps registered (use 'camp init' to create one)")
	}

	home, _ := pathutil.Home()
	// A named --campaign is the choice. A bare --campaign still has to be confirmed.
	open := linkOpen{
		ctx:       ctx,
		name:      flags.name,
		nameSet:   strings.TrimSpace(flags.name) != "",
		camp:      selected,
		hasCamp:   hasSelected,
		camps:     camps,
		forceCamp: flags.forceCamp && flags.campaign == "",
		noCommit:  flags.noCommit,
		home:      home,
		homes:     linkHomeAliases(home),
	}

	if len(args) > 0 {
		open.chosen = pathutil.ExpandHome(args[0])
		return open, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return linkOpen{}, err
	}
	inside := hasSelected && linkInsideCamp(cwd, selected.Path)
	open.browse, open.offerHere = linkBrowseFrom(cwd, selected.Path, home, inside)
	if cleaned, err := cleanLinkDir(open.browse); err == nil {
		open.browse = cleaned
	}
	return open, nil
}

func detectCurrentCamp(ctx context.Context) (linkCamp, bool) {
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil || cfg == nil || cfg.ID == "" {
		return linkCamp{}, false
	}
	return campFromConfig(cfg, root), true
}

func campFromConfig(cfg *config.CampaignConfig, root string) linkCamp {
	name := ""
	id := ""
	if cfg != nil {
		name = cfg.Name
		id = cfg.ID
	}
	if name == "" {
		name = filepath.Base(root)
	}
	return linkCamp{ID: id, Name: name, Path: root}
}

func loadLinkCamps(ctx context.Context) ([]linkCamp, error) {
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "load registry")
	}
	out := make([]linkCamp, 0, reg.Len())
	for _, entry := range reg.ListAll() {
		path := strings.TrimSpace(entry.Path)
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		name := entry.Name
		if name == "" {
			name = filepath.Base(path)
		}
		out = append(out, linkCamp{ID: entry.ID, Name: name, Path: path})
	}
	slicesSortCamps(out)
	return out, nil
}

func slicesSortCamps(camps []linkCamp) {
	slices.SortFunc(camps, func(a, b linkCamp) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.Path, b.Path),
		)
	})
}

func ensureLinkCamp(camps []linkCamp, camp linkCamp) []linkCamp {
	for _, existing := range camps {
		if camp.ID != "" && existing.ID == camp.ID {
			return camps
		}
		if existing.Path != "" && existing.Path == camp.Path {
			return camps
		}
	}
	camps = append(camps, camp)
	slicesSortCamps(camps)
	return camps
}

// linkInsideCamp reports whether cwd is the camp root or a directory inside it.
func linkInsideCamp(cwd, campRoot string) bool {
	if strings.TrimSpace(campRoot) == "" {
		return false
	}
	return isWithinTargetCampaign(cwd, campRoot)
}

// linkBrowseFrom chooses the directory the browser opens on.
// Inside a camp, start at the user's home so the camp's own tree is not the
// list of projects to link. The bool is true when that directory itself is
// the project being offered.
func linkBrowseFrom(cwd, campRoot, home string, inside bool) (string, bool) {
	if inside {
		if strings.TrimSpace(home) != "" {
			return home, false
		}
		if strings.TrimSpace(campRoot) != "" {
			return filepath.Dir(campRoot), false
		}
	}
	return cwd, true
}

func newLinkModel(open linkOpen) linkModel {
	name := newLinkInput("my-project")
	if open.nameSet {
		name.SetValue(open.name)
	}
	path := newLinkInput("paste or type a path")
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = lipgloss.NewStyle().Foreground(linkPal.Accent)

	m := linkModel{
		ctx:        open.ctx,
		noCommit:   open.noCommit,
		homes:      open.homes,
		camps:      open.camps,
		camp:       open.camp,
		hasCamp:    open.hasCamp,
		campPicked: open.hasCamp && !open.forceCamp,
		forceCamp:  open.forceCamp,
		cwd:        open.browse,
		offerHere:  open.offerHere,
		preset:     open.chosen != "",
		nameInput:  name,
		pathInput:  path,
		nameSet:    open.nameSet,
		chosenPath: open.chosen,
		spin:       spin,
		listFn:     readLinkDirs,
		statFn:     defaultLinkStat,
		planFn: func(ctx context.Context, root, path, name string) (*projectsvc.LinkPlan, error) {
			return projectsvc.PlanLinked(ctx, root, path, projectsvc.LinkOptions{Name: name})
		},
		applyFn: applyProjectLink,
	}
	if open.listFn != nil {
		m.listFn = open.listFn
	}
	if open.statFn != nil {
		m.statFn = open.statFn
	}
	if open.planFn != nil {
		m.planFn = open.planFn
	}
	if open.applyFn != nil {
		m.applyFn = open.applyFn
	}
	slicesSortCamps(m.camps)
	if m.cwd == "" {
		m.cwd = open.chosen
	}
	switch {
	case open.chosen == "":
		m.step = stepFolder
		m.anchor = m.cwd
		m.anchorOffer = m.offerHere
		m = m.loadDir()
		m.pathInput.Focus()
	case !open.nameSet:
		m.step = stepName
		m.chosenPath = open.chosen
		m.nameInput.SetValue(filepath.Base(open.chosen))
	case m.needsCamp():
		m.step = stepCamp
		m.chosenPath = open.chosen
		m.chosenName = strings.TrimSpace(open.name)
		m = m.focusCamp()
	default:
		m.step = stepReview
		m.chosenPath = open.chosen
		m.chosenName = strings.TrimSpace(open.name)
		m.planning = true
		m.planGen = 1
	}
	m.rebuildCamps()
	return m
}

func newLinkInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 512
	ti.Width = 48
	ti.Prompt = "› "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(linkPal.Accent).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(linkPal.TextPrimary)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(linkPal.TextMuted)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(linkPal.Accent)
	return ti
}

func (m linkModel) needsCamp() bool {
	if m.forceCamp && !m.campPicked {
		return true
	}
	return !m.hasCamp
}

func (m linkModel) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink}
	switch m.step {
	case stepFolder:
		cmds = append(cmds, m.pathInput.Focus())
	case stepName:
		cmds = append(cmds, m.nameInput.Focus())
	}
	if m.planning {
		cmds = append(cmds, m.planCmd())
	}
	return tea.Batch(cmds...)
}

func (m linkModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.fitInputs()
		return m, nil
	case spinner.TickMsg:
		if m.step != stepWork {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case linkPlannedMsg:
		return m.planned(msg)
	case linkAppliedMsg:
		return m.applied(msg)
	case tea.KeyMsg:
		return m.onKey(msg)
	default:
		if m.step == stepFolder {
			return m.absorbPathMsg(msg)
		}
	}
	return m, nil
}

func (m linkModel) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" && m.step != stepWork {
		m.quitting = true
		return m, tea.Quit
	}
	switch m.step {
	case stepFolder:
		return m.onBrowseKey(msg)
	case stepName:
		return m.onNameKey(msg)
	case stepCamp:
		return m.onCampKey(msg)
	case stepReview:
		return m.onReviewKey(msg)
	case stepDone:
		switch msg.String() {
		case "enter", "esc", "q":
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m linkModel) onBrowseKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Paste {
		return m.pastePath(string(msg.Runes))
	}
	switch msg.String() {
	case "esc":
		if strings.TrimSpace(m.pathInput.Value()) == "" {
			m.quitting = true
			return m, tea.Quit
		}
		m.pathInput.SetValue("")
		m.errMsg = ""
		return m.showAnchor(), nil
	case "ctrl+u":
		m.pathInput.SetValue("")
		m.errMsg = ""
		return m.showAnchor(), nil
	case "up":
		return m.move(-1), nil
	case "down":
		return m.move(1), nil
	case "tab", "right":
		return m.openHighlighted()
	case "enter":
		return m.submitBrowse()
	default:
		return m.absorbPathMsg(msg)
	}
}

func (m linkModel) absorbPathMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := m.pathInput.Value()
	var cmd tea.Cmd
	m.pathInput, cmd = m.pathInput.Update(msg)
	m.rewritePathField()
	if m.pathInput.Value() == before {
		return m, cmd
	}
	return m.applyPathText(), cmd
}

func (m linkModel) pastePath(raw string) (tea.Model, tea.Cmd) {
	cleaned, submit := normalizePastedPath(raw)
	m.pathInput.SetValue(cleaned)
	m.pathInput.CursorEnd()
	m = m.applyPathText()
	if submit {
		return m.submitBrowse()
	}
	return m, nil
}

func (m linkModel) submitBrowse() (tea.Model, tea.Cmd) {
	raw := m.pathInput.Value()
	trimmed := trimTrailingSlashField(raw)
	if trimmed != raw {
		m.pathInput.SetValue(trimmed)
		m = m.applyPathText()
	}
	if strings.TrimSpace(m.pathInput.Value()) == "" {
		return m.activateHighlighted()
	}
	if cleaned, ok := m.lookupPath(m.pathInput.Value()); ok {
		if isDir, err := m.statFn(cleaned); err == nil && isDir {
			return m.choosePath(cleaned)
		}
	}
	if dir, ok := m.onlyVisibleDir(); ok {
		return m.choosePath(dir.path)
	}
	if entry, ok := m.selected(); ok && entry.kind == "dir" {
		return m.choosePath(entry.path)
	}
	m.errMsg = "folder not found"
	return m, nil
}

func (m linkModel) activateHighlighted() (tea.Model, tea.Cmd) {
	entry, ok := m.selected()
	if !ok {
		return m, nil
	}
	switch entry.kind {
	case "here", "dir":
		return m.choosePath(entry.path)
	case "up":
		return m.goUp(), nil
	case "place":
		return m.jumpTo(entry.path), nil
	default:
		return m.choosePath(entry.path)
	}
}

func (m linkModel) openHighlighted() (tea.Model, tea.Cmd) {
	entry, ok := m.selected()
	if !ok {
		return m, nil
	}
	switch entry.kind {
	case "here":
		return m, nil
	case "up":
		return m.goUp(), nil
	case "place":
		return m.jumpTo(entry.path), nil
	default:
		return m.descend(entry), nil
	}
}

func (m linkModel) choosePath(path string) (tea.Model, tea.Cmd) {
	m.chosenPath = path
	m.errMsg = ""
	m.query = ""
	m.showHidden = false
	m.pathInput.SetValue("")
	if m.cwd != "" {
		m.anchor = m.cwd
		m.anchorOffer = m.offerHere
	}
	if !m.nameSet {
		m.nameInput.SetValue(filepath.Base(path))
	}
	m.step = stepName
	return m, m.nameInput.Focus()
}

func (m linkModel) descend(entry linkEntry) linkModel {
	m.cwd = entry.path
	m.anchor = entry.path
	m.offerHere = entry.badge != ""
	m.anchorOffer = m.offerHere
	m.query = ""
	m.showHidden = false
	m.errMsg = ""
	m.pathInput.SetValue("")
	return m.loadDir()
}

func (m linkModel) goUp() linkModel {
	parent := filepath.Dir(m.cwd)
	if parent == m.cwd {
		return m
	}
	child := filepath.Base(m.cwd)
	m.cwd = parent
	m.anchor = parent
	m.offerHere = false
	m.anchorOffer = false
	m.query = ""
	m.showHidden = false
	m.errMsg = ""
	m.pathInput.SetValue("")
	m = m.loadDir()
	for i, entry := range m.visible {
		if entry.kind == "dir" && entry.label == child {
			m.cursor = i
			break
		}
	}
	return m
}

func (m linkModel) jumpTo(path string) linkModel {
	m.cwd = path
	m.anchor = path
	m.offerHere = false
	m.anchorOffer = false
	m.query = ""
	m.showHidden = false
	m.errMsg = ""
	m.pathInput.SetValue("")
	return m.loadDir()
}

func (m linkModel) onNameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.errMsg = ""
		if m.preset {
			m.quitting = true
			return m, tea.Quit
		}
		m.step = stepFolder
		m.query = ""
		m.showHidden = false
		m.pathInput.SetValue("")
		return m.loadDir(), m.pathInput.Focus()
	case "enter":
		return m.submitName()
	default:
		var cmd tea.Cmd
		m.nameInput, cmd = m.nameInput.Update(msg)
		return m, cmd
	}
}

func (m linkModel) submitName() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.nameInput.Value())
	if err := projectsvc.ValidateProjectName(name); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.chosenName = name
	m.errMsg = ""
	if m.needsCamp() {
		m.step = stepCamp
		return m.focusCamp(), nil
	}
	return m.beginReview()
}

func (m linkModel) onCampKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.campFiltering {
		switch msg.String() {
		case "esc":
			m.campFiltering = false
			m.campQuery = ""
			m.rebuildCamps()
			return m, nil
		case "enter":
			m.campFiltering = false
			return m.selectCamp()
		case "up":
			return m.moveCamp(-1), nil
		case "down":
			return m.moveCamp(1), nil
		case "backspace":
			if m.campQuery == "" {
				m.campFiltering = false
				m.rebuildCamps()
				return m, nil
			}
			runes := []rune(m.campQuery)
			m.campQuery = string(runes[:len(runes)-1])
			m.rebuildCamps()
			return m, nil
		default:
			if linkPrintable(msg) {
				m.campQuery += msg.String()
				m.rebuildCamps()
			}
			return m, nil
		}
	}
	switch msg.String() {
	case "q":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.errMsg = ""
		if m.campFromReview {
			m.campFromReview = false
			m.camp = m.campBackup
			m.hasCamp = m.hadCamp
			m.step = stepReview
			return m, nil
		}
		m.step = stepName
		return m, m.nameInput.Focus()
	case "up", "k":
		return m.moveCamp(-1), nil
	case "down", "j":
		return m.moveCamp(1), nil
	case "/":
		m.campFiltering = true
		m.campQuery = ""
		m.rebuildCamps()
		return m, nil
	case "enter":
		return m.selectCamp()
	}
	return m, nil
}

func (m linkModel) selectCamp() (tea.Model, tea.Cmd) {
	camp, ok := m.selectedCamp()
	if !ok {
		m.errMsg = "choose a camp"
		return m, nil
	}
	m.camp = camp
	m.hasCamp = true
	m.campPicked = true
	m.forceCamp = false
	m.campFromReview = false
	m.errMsg = ""
	if m.chosenPath == "" || m.chosenName == "" {
		m.step = stepName
		return m, m.nameInput.Focus()
	}
	return m.beginReview()
}

func (m linkModel) onReviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.errMsg = ""
		m.planning = false
		m.step = stepName
		return m, m.nameInput.Focus()
	case "c":
		if m.planning {
			return m, nil
		}
		m.campBackup = m.camp
		m.hadCamp = m.hasCamp
		m.campFromReview = true
		m.step = stepCamp
		return m.focusCamp(), nil
	case "enter":
		return m.confirm()
	}
	return m, nil
}

func (m linkModel) confirm() (tea.Model, tea.Cmd) {
	if m.planning || m.plan == nil {
		return m, nil
	}
	m.step = stepWork
	m.errMsg = ""
	return m, tea.Batch(m.spin.Tick, m.applyCmd())
}

func (m linkModel) beginReview() (tea.Model, tea.Cmd) {
	m.planGen++
	m.step = stepReview
	m.plan = nil
	m.planning = true
	m.errMsg = ""
	return m, m.planCmd()
}

func (m linkModel) planned(msg linkPlannedMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.planGen || m.step != stepReview {
		return m, nil
	}
	m.planning = false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		m.plan = nil
		m.step = stepName
		return m, m.nameInput.Focus()
	}
	m.plan = msg.plan
	m.chosenName = msg.plan.Name
	m.errMsg = ""
	return m, nil
}

func (m linkModel) applied(msg linkAppliedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.step = stepReview
		m.errMsg = msg.err.Error()
		return m, nil
	}
	m.result = msg.result
	m.note = msg.note
	m.step = stepDone
	m.errMsg = ""
	return m, nil
}

func (m linkModel) planCmd() tea.Cmd {
	gen := m.planGen
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	root, path, name := m.camp.Path, m.chosenPath, m.chosenName
	fn := m.planFn
	return func() tea.Msg {
		plan, err := fn(ctx, root, path, name)
		return linkPlannedMsg{plan: plan, err: err, gen: gen}
	}
}

func (m linkModel) applyCmd() tea.Cmd {
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	camp, plan, noCommit, fn := m.camp, m.plan, m.noCommit, m.applyFn
	return func() tea.Msg {
		result, note, err := fn(ctx, camp, plan, noCommit)
		return linkAppliedMsg{result: result, note: note, err: err}
	}
}

func applyProjectLink(ctx context.Context, camp linkCamp, plan *projectsvc.LinkPlan, noCommit bool) (*projectsvc.LinkResult, linkCommitNote, error) {
	result, err := projectsvc.ApplyLinked(ctx, plan)
	if err != nil {
		return nil, linkCommitNote{}, err
	}
	note := linkCommitNote{}
	if !noCommit {
		cfg, cfgErr := config.LoadCampaignConfig(ctx, camp.Path)
		if cfgErr != nil {
			note.Message = cfgErr.Error()
		} else {
			commitResult := CommitLink(ctx, cfg, camp.Path, result.Path, result.Name)
			note.Committed = commitResult.Committed
			note.Message = commitResult.Message
			if commitResult.Err != nil && note.Message == "" {
				note.Message = commitResult.Err.Error()
			}
		}
	}
	if camp.ID != "" {
		_ = config.UpdateRegistry(ctx, func(reg *config.Registry) error {
			reg.UpdateLastAccess(camp.ID)
			return nil
		})
	}
	return result, note, nil
}

func (m *linkModel) fitInputs() {
	w := m.width - 8
	if w < 16 {
		w = 16
	}
	if w > 56 {
		w = 56
	}
	m.nameInput.Width = w
	m.pathInput.Width = w
}

func (m linkModel) loadDir() linkModel {
	children, err := m.listFn(m.cwd, m.showHidden)
	if err != nil {
		m.errMsg = err.Error()
		m.dirs = nil
	} else {
		m.errMsg = ""
		m.dirs = children
	}
	m.rebuildVisible()
	m.cursor = m.defaultCursor()
	return m
}

func (m linkModel) defaultCursor() int {
	if m.offerHere || len(m.visible) == 0 {
		return 0
	}
	for i, entry := range m.visible {
		if entry.kind == "dir" {
			return i
		}
	}
	return 0
}

func (m *linkModel) rebuildVisible() {
	dirs := m.dirs
	q := strings.ToLower(strings.TrimSpace(m.query))
	if q != "" {
		matched := make([]linkEntry, 0, len(dirs))
		for _, entry := range dirs {
			if strings.Contains(strings.ToLower(entry.label), q) {
				matched = append(matched, entry)
			}
		}
		dirs = matched
	}
	m.visible = append(m.pinned(), dirs...)
	if len(m.visible) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m linkModel) pinned() []linkEntry {
	here := linkEntry{
		label: "Link this folder",
		path:  m.cwd,
		kind:  "here",
		badge: linkDirBadge(m.cwd),
	}
	pins := []linkEntry{here}
	parent := filepath.Dir(m.cwd)
	if parent != m.cwd {
		pins = append(pins, linkEntry{label: "..", path: parent, kind: "up"})
	}
	if strings.TrimSpace(m.query) == "" {
		pins = append(pins, m.places()...)
	}
	return pins
}

func (m linkModel) places() []linkEntry {
	var out []linkEntry
	if home := m.homePath(); home != "" && !m.listingHome() {
		out = append(out, linkEntry{label: "Home", path: home, kind: "place"})
	}
	const disks = "/Volumes"
	if m.statFn != nil && m.cwd != disks {
		if ok, err := m.statFn(disks); err == nil && ok {
			out = append(out, linkEntry{label: "Disks", path: disks, kind: "place"})
		}
	}
	if m.cwd != "/" {
		out = append(out, linkEntry{label: "/", path: "/", kind: "place"})
	}
	return out
}

func (m linkModel) homePath() string {
	if len(m.homes) == 0 {
		return ""
	}
	return m.homes[0]
}

func (m linkModel) listingHome() bool {
	for _, home := range m.homes {
		if home != "" && m.cwd == home {
			return true
		}
	}
	return false
}

func (m linkModel) applyPathText() linkModel {
	m.errMsg = ""
	raw := strings.TrimSpace(m.pathInput.Value())
	if raw == "" {
		return m.showAnchor()
	}
	cleaned, ok := m.lookupPath(raw)
	if !ok {
		return m.showAnchor()
	}
	if m.statFn != nil {
		if isDir, err := m.statFn(cleaned); err == nil && isDir {
			return m.showDir(cleaned)
		}
	}
	parent := filepath.Dir(cleaned)
	if parent == cleaned || m.statFn == nil {
		return m.showAnchor()
	}
	if isDir, err := m.statFn(parent); err != nil || !isDir {
		return m.showAnchor()
	}
	return m.showFilter(parent, filepath.Base(cleaned))
}

func (m linkModel) showAnchor() linkModel {
	if m.anchor == "" {
		m.anchor = m.cwd
		m.anchorOffer = m.offerHere
	}
	if m.cwd == m.anchor && m.query == "" && !m.showHidden && m.offerHere == m.anchorOffer {
		return m
	}
	m.query = ""
	m.showHidden = false
	m.cwd = m.anchor
	m.offerHere = m.anchorOffer
	return m.loadDir()
}

func (m linkModel) showDir(dir string) linkModel {
	if m.cwd == dir && m.query == "" && m.offerHere && !m.showHidden {
		return m
	}
	m.cwd = dir
	m.query = ""
	m.showHidden = false
	m.offerHere = true
	return m.loadDir()
}

func (m linkModel) showFilter(parent, base string) linkModel {
	hidden := strings.HasPrefix(base, ".")
	if m.cwd == parent && m.query == base && m.showHidden == hidden && !m.offerHere {
		m.rebuildVisible()
		m.cursor = m.defaultCursor()
		return m
	}
	m.cwd = parent
	m.query = base
	m.showHidden = hidden
	m.offerHere = false
	return m.loadDir()
}

func (m linkModel) lookupPath(raw string) (string, bool) {
	expanded := pathutil.ExpandHome(strings.TrimSpace(stripFileURL(stripWrappingQuotes(strings.TrimSpace(raw)))))
	expanded = trimTrailingSlashField(expanded)
	if expanded == "" {
		return "", false
	}
	if !filepath.IsAbs(expanded) {
		base := m.anchor
		if base == "" {
			base = m.cwd
		}
		expanded = filepath.Join(base, expanded)
	}
	cleaned, err := cleanLinkDir(expanded)
	if err != nil {
		return "", false
	}
	return cleaned, true
}

func (m linkModel) onlyVisibleDir() (linkEntry, bool) {
	var found linkEntry
	count := 0
	for _, entry := range m.visible {
		if entry.kind != "dir" {
			continue
		}
		count++
		found = entry
		if count > 1 {
			return linkEntry{}, false
		}
	}
	return found, count == 1
}

func (m *linkModel) rewritePathField() {
	raw := m.pathInput.Value()
	cleaned := rewritePathText(raw)
	if cleaned == raw {
		return
	}
	m.pathInput.SetValue(cleaned)
	m.pathInput.CursorEnd()
}

func rewritePathText(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}
	if stripped := stripFileURL(trimmed); stripped != trimmed {
		return stripped
	}
	if unquoted := stripWrappingQuotes(trimmed); unquoted != trimmed {
		return unquoted
	}
	return raw
}

func normalizePastedPath(raw string) (string, bool) {
	submit := strings.ContainsAny(raw, "\r\n")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	if line, _, ok := strings.Cut(raw, "\n"); ok {
		raw = line
	}
	raw = strings.TrimSpace(raw)
	raw = stripWrappingQuotes(raw)
	raw = strings.TrimSpace(raw)
	raw = stripFileURL(raw)
	return raw, submit
}

func stripWrappingQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func stripFileURL(s string) string {
	rest, ok := strings.CutPrefix(s, "file://localhost")
	if !ok {
		rest, ok = strings.CutPrefix(s, "file://")
	}
	if !ok || !strings.HasPrefix(rest, "/") {
		return s
	}
	if unescaped, err := url.PathUnescape(rest); err == nil {
		return unescaped
	}
	return rest
}

func trimTrailingSlashField(s string) string {
	if s == "" || s == "/" || s == "~" {
		return s
	}
	return strings.TrimRight(s, `/\`)
}

func (m linkModel) move(delta int) linkModel {
	n := len(m.visible)
	if n == 0 {
		return m
	}
	m.cursor = (m.cursor + delta + n) % n
	return m
}

func (m linkModel) selected() (linkEntry, bool) {
	if len(m.visible) == 0 || m.cursor < 0 || m.cursor >= len(m.visible) {
		return linkEntry{}, false
	}
	return m.visible[m.cursor], true
}

func (m *linkModel) rebuildCamps() {
	q := strings.ToLower(strings.TrimSpace(m.campQuery))
	if q == "" {
		m.campVisible = append([]linkCamp(nil), m.camps...)
	} else {
		matched := make([]linkCamp, 0, len(m.camps))
		for _, camp := range m.camps {
			if strings.Contains(strings.ToLower(camp.Name), q) || strings.Contains(strings.ToLower(camp.Path), q) {
				matched = append(matched, camp)
			}
		}
		m.campVisible = matched
	}
	if len(m.campVisible) == 0 {
		m.campCursor = 0
		return
	}
	if m.campCursor >= len(m.campVisible) {
		m.campCursor = len(m.campVisible) - 1
	}
	if m.campCursor < 0 {
		m.campCursor = 0
	}
}

func (m linkModel) focusCamp() linkModel {
	m.campQuery = ""
	m.campFiltering = false
	m.rebuildCamps()
	if m.hasCamp {
		for i, camp := range m.campVisible {
			if camp.Path == m.camp.Path && (m.camp.ID == "" || camp.ID == m.camp.ID) {
				m.campCursor = i
				break
			}
		}
	}
	return m
}

func (m linkModel) moveCamp(delta int) linkModel {
	n := len(m.campVisible)
	if n == 0 {
		return m
	}
	m.campCursor = (m.campCursor + delta + n) % n
	return m
}

func (m linkModel) selectedCamp() (linkCamp, bool) {
	if len(m.campVisible) == 0 || m.campCursor < 0 || m.campCursor >= len(m.campVisible) {
		return linkCamp{}, false
	}
	return m.campVisible[m.campCursor], true
}

func readLinkDirs(path string, showHidden bool) ([]linkEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]linkEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		child := filepath.Join(path, name)
		info, err := os.Stat(child)
		if err != nil || !info.IsDir() {
			continue
		}
		out = append(out, linkEntry{
			label: name,
			path:  child,
			kind:  "dir",
			badge: linkDirBadge(child),
		})
	}
	slices.SortFunc(out, func(a, b linkEntry) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.label), strings.ToLower(b.label)),
			cmp.Compare(a.label, b.label),
		)
	})
	return out, nil
}

func linkDirBadge(path string) string {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
		return ""
	}
	switch {
	case linkExists(filepath.Join(path, "go.mod")):
		return "go"
	case linkExists(filepath.Join(path, "Cargo.toml")):
		return "rust"
	case linkExists(filepath.Join(path, "package.json")):
		return "node"
	case linkExists(filepath.Join(path, "pyproject.toml")), linkExists(filepath.Join(path, "setup.py")):
		return "python"
	default:
		return "git"
	}
}

func linkExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func defaultLinkStat(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func cleanLinkDir(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", camperrors.Wrap(camperrors.ErrInvalidInput, "link path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

func linkHomeAliases(home string) []string {
	home = strings.TrimSpace(home)
	if home == "" {
		return nil
	}
	out := []string{home}
	if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != "" && resolved != home {
		out = append(out, resolved)
	}
	return out
}

func linkShortPath(path string, homes []string) string {
	for _, home := range homes {
		if home == "" {
			continue
		}
		if path == home {
			return "~"
		}
		prefix := home + string(filepath.Separator)
		if rest, ok := strings.CutPrefix(path, prefix); ok {
			return "~" + string(filepath.Separator) + rest
		}
	}
	return path
}

func linkPrintable(key tea.KeyMsg) bool {
	if strings.HasPrefix(key.String(), "ctrl+") {
		return false
	}
	if key.Type == tea.KeyRunes && len(key.Runes) > 0 {
		return unicode.IsPrint(key.Runes[0])
	}
	rs := []rune(key.String())
	return len(rs) == 1 && unicode.IsPrint(rs[0])
}
