package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/concept"
	"github.com/Obedience-Corp/camp/internal/state"
	"github.com/Obedience-Corp/camp/internal/tui/selector"
	tea "github.com/charmbracelet/bubbletea"
)

type pickerStep int

const (
	stepSelectingType pickerStep = iota
	stepSelectingItem
	stepDone
)

const (
	noneOptionID    = "(none)"
	noneOptionLabel = "(none) - No concept association"
	newProjectLabel = "+ New Project"
	newProjectPath  = "projects/new"

	pickerHelpLong  = "↑/↓ move · type to filter · enter select · backspace back · esc cancel"
	pickerHelpShort = "↑/↓ · type · enter · esc"
	pickerHelpDrill = "↑/↓ move · type to filter · enter select · → drill · backspace back · esc cancel"
)

// ConceptPickerModel is a cascading concept picker backed by selector.Model.
type ConceptPickerModel struct {
	conceptSvc   concept.Service
	concepts     []concept.Concept
	items        []concept.Item
	sel          selector.Model
	step         pickerStep
	ranks        map[string]time.Time
	campaignRoot string

	selectedConcept *concept.Concept
	currentSubpath  string
	pathHistory     []string

	selectedPath string
	cancelled    bool
	ctx          context.Context
}

func NewConceptPickerModel(ctx context.Context, svc concept.Service, campaignRoot string) ConceptPickerModel {
	concepts, err := svc.List(ctx)
	if err != nil {
		concepts = nil
	}
	m := ConceptPickerModel{
		ctx:          ctx,
		conceptSvc:   svc,
		concepts:     concepts,
		campaignRoot: campaignRoot,
		ranks:        loadPickerRanks(ctx, campaignRoot),
		step:         stepSelectingType,
	}
	m.sel = selector.New(m.typeItems(), m.typeOpts())
	return m
}

func (m ConceptPickerModel) Init() tea.Cmd { return nil }

func (m ConceptPickerModel) Update(msg tea.Msg) (ConceptPickerModel, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch m.step {
	case stepSelectingType:
		return m.updateTypeSelection(key)
	case stepSelectingItem:
		return m.updateItemSelection(key)
	}
	return m, nil
}

func (m ConceptPickerModel) updateTypeSelection(msg tea.KeyMsg) (ConceptPickerModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return m.confirmType()
	case "esc", "backspace", "left", "h":
		if !m.sel.Filtering() {
			m.cancelled = true
			m.step = stepDone
			return m, nil
		}
	}
	m.sel, _ = m.sel.Update(msg)
	return m, nil
}

func (m ConceptPickerModel) confirmType() (ConceptPickerModel, tea.Cmd) {
	it, ok := m.sel.Selected()
	if !ok {
		return m, nil
	}
	if it.ID == noneOptionID {
		m.selectedPath = ""
		m.step = stepDone
		return m, nil
	}
	for i := range m.concepts {
		if m.concepts[i].Name == it.ID {
			c := m.concepts[i]
			m.selectedConcept = &c
			if c.HasItems {
				m.step = stepSelectingItem
				m.loadItems("")
			} else {
				m.selectedPath = c.Path
				m.step = stepDone
			}
			return m, nil
		}
	}
	return m, nil
}

func (m ConceptPickerModel) updateItemSelection(msg tea.KeyMsg) (ConceptPickerModel, tea.Cmd) {
	filtering := m.sel.Filtering()
	switch msg.String() {
	case "enter":
		return m.confirmOrDrillItem(false)
	case "right", "l":
		if !filtering {
			return m.confirmOrDrillItem(true)
		}
	case "backspace", "left", "h":
		if !filtering {
			m.navigateUp()
			return m, nil
		}
	case "esc":
		if !filtering {
			m.cancelled = true
			m.step = stepDone
			return m, nil
		}
	}
	m.sel, _ = m.sel.Update(msg)
	if m.sel.Consumed() {
		return m, nil
	}
	return m, nil
}

func (m ConceptPickerModel) confirmOrDrillItem(forceDrill bool) (ConceptPickerModel, tea.Cmd) {
	it, ok := m.sel.Selected()
	if !ok || m.selectedConcept == nil {
		return m, nil
	}
	item, found := m.itemByPath(it.ID)
	if !found {
		return m, nil
	}
	infiniteDepth := m.selectedConcept.MaxDepth == nil
	canDrill := item.IsDir && item.Children > 0 && !item.DrillDisabled
	if forceDrill && canDrill {
		m.drillInto(item)
		return m, nil
	}
	if canDrill && !infiniteDepth {
		m.drillInto(item)
		return m, nil
	}
	m.selectedPath = item.Path
	m.recordVisit(item.Path)
	m.step = stepDone
	return m, nil
}

func (m *ConceptPickerModel) drillInto(item concept.Item) {
	m.pathHistory = append(m.pathHistory, m.currentSubpath)
	if m.currentSubpath == "" {
		m.currentSubpath = item.Name
	} else {
		m.currentSubpath = m.currentSubpath + "/" + item.Name
	}
	m.loadItems(m.currentSubpath)
}

func (m *ConceptPickerModel) navigateUp() {
	switch {
	case len(m.pathHistory) > 0:
		lastIdx := len(m.pathHistory) - 1
		previousPath := m.pathHistory[lastIdx]
		m.pathHistory = m.pathHistory[:lastIdx]
		m.currentSubpath = previousPath
		m.loadItems(previousPath)
	case m.currentSubpath != "":
		m.currentSubpath = ""
		m.loadItems("")
	default:
		m.step = stepSelectingType
		m.selectedConcept = nil
		m.items = nil
		m.currentSubpath = ""
		m.sel.Reset(m.typeItems(), m.typeOpts())
	}
}

func (m *ConceptPickerModel) loadItems(subpath string) {
	if m.selectedConcept == nil {
		return
	}
	items, err := m.conceptSvc.ListItems(m.ctx, m.selectedConcept.Name, subpath)
	if err != nil {
		m.items = nil
		m.sel.Reset(nil, m.itemOpts())
		return
	}
	if isProjectsConcept(m.selectedConcept) && subpath == "" {
		items = append([]concept.Item{{
			Name:          newProjectLabel,
			Path:          newProjectPath,
			IsDir:         false,
			Children:      0,
			DrillDisabled: true,
		}}, items...)
	}
	m.items = items
	m.sel.Reset(m.itemSelectorItems(), m.itemOpts())
}

func (m ConceptPickerModel) View() string {
	switch m.step {
	case stepSelectingType, stepSelectingItem:
		return m.sel.View()
	default:
		return ""
	}
}

func (m ConceptPickerModel) Done() bool { return m.step == stepDone }

func (m ConceptPickerModel) Cancelled() bool { return m.cancelled }

func (m ConceptPickerModel) SelectedPath() string { return m.selectedPath }

func (m ConceptPickerModel) SelectedConcept() *concept.Concept { return m.selectedConcept }

func (m ConceptPickerModel) typeItems() []selector.Item {
	items := make([]selector.Item, 0, len(m.concepts)+1)
	items = append(items, selector.Item{
		ID:    noneOptionID,
		Label: noneOptionLabel,
		Pin:   selector.PinTop,
	})
	for _, c := range m.concepts {
		items = append(items, selector.Item{
			ID:     c.Name,
			Label:  c.Name + " - " + c.Description,
			Filter: c.Path,
			Rank:   state.RankTime(m.ranks, c.Path),
		})
	}
	return items
}

func (m ConceptPickerModel) typeOpts() selector.Options {
	opts := selector.Options{
		Title:     "Select concept type:",
		Help:      pickerHelpLong,
		HelpShort: pickerHelpShort,
		InitialID: noneOptionID,
	}
	if id := mostRecentID(m.typeItems()); id != "" {
		opts.InitialID = id
	}
	return opts
}

func (m ConceptPickerModel) itemSelectorItems() []selector.Item {
	out := make([]selector.Item, 0, len(m.items))
	for _, item := range m.items {
		sel := selector.Item{
			ID:     item.Path,
			Label:  itemLabel(item),
			Filter: item.Path,
			Rank:   state.RankTime(m.ranks, item.Path),
		}
		if item.Name == newProjectLabel {
			sel.Pin = selector.PinTop
			sel.Rank = time.Time{}
		}
		out = append(out, sel)
	}
	return out
}

func (m ConceptPickerModel) itemOpts() selector.Options {
	help := pickerHelpLong
	if m.selectedConcept != nil && m.selectedConcept.MaxDepth == nil {
		help = pickerHelpDrill
	}
	return selector.Options{
		Title:     "📁 " + m.buildBreadcrumb(),
		Help:      help,
		HelpShort: pickerHelpShort,
	}
}

func (m ConceptPickerModel) itemByPath(path string) (concept.Item, bool) {
	for _, item := range m.items {
		if item.Path == path {
			return item, true
		}
	}
	return concept.Item{}, false
}

func (m ConceptPickerModel) recordVisit(rel string) {
	if m.campaignRoot == "" || rel == "" || rel == newProjectPath {
		return
	}
	abs := rel
	if !filepath.IsAbs(rel) {
		abs = filepath.Join(m.campaignRoot, filepath.FromSlash(rel))
	}
	_ = state.RecordVisit(m.ctx, m.campaignRoot, abs)
}

func (m ConceptPickerModel) buildBreadcrumb() string {
	if m.selectedConcept == nil {
		return ""
	}
	parts := []string{m.selectedConcept.Name}
	if m.currentSubpath != "" {
		parts = append(parts, strings.Split(m.currentSubpath, "/")...)
	}
	return strings.Join(parts, " > ")
}

func itemLabel(item concept.Item) string {
	switch {
	case item.Name == newProjectLabel:
		return "✨ " + item.Name
	case item.IsDir && item.DrillDisabled:
		return item.Name
	case item.IsDir && item.Children > 0:
		return "▸ " + item.Name
	case item.IsDir:
		return item.Name + " (empty)"
	default:
		return item.Name
	}
}

func isProjectsConcept(c *concept.Concept) bool {
	if c == nil {
		return false
	}
	if c.Name == "p" || c.Name == "projects" {
		return true
	}
	path := strings.Trim(strings.TrimSuffix(filepath.ToSlash(c.Path), "/"), "/")
	return path == "projects"
}

func loadPickerRanks(ctx context.Context, campaignRoot string) map[string]time.Time {
	if campaignRoot == "" {
		return nil
	}
	entries, err := state.LoadHistory(ctx, campaignRoot)
	if err != nil {
		return nil
	}
	ranks := state.RankMap(entries, campaignRoot)
	overlayCwd(ranks, campaignRoot)
	return ranks
}

func overlayCwd(ranks map[string]time.Time, campaignRoot string) {
	if ranks == nil {
		return
	}
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return
	}
	rel, err := filepath.Rel(filepath.Clean(campaignRoot), filepath.Clean(cwd))
	if err != nil {
		return
	}
	rel = filepath.ToSlash(rel)
	rel = strings.Trim(rel, "/")
	if rel == "" || rel == "." || strings.Contains(rel, "..") {
		return
	}
	ranks[rel] = time.Now()
}

func mostRecentID(items []selector.Item) string {
	var best time.Time
	id := ""
	for _, it := range items {
		if it.Rank.IsZero() {
			continue
		}
		if best.IsZero() || it.Rank.After(best) {
			best = it.Rank
			id = it.ID
		}
	}
	return id
}
