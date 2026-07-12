package festivals

import (
	"context"
	"os"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fest"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// Overridden in tests so dispatch does not depend on the runner's terminal.
var stdoutIsTTY = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

// festivalsTUIRequested decides whether bare `camp festivals` opens the
// browser. Machine output (--json) never does; -i forces; otherwise an
// interactive terminal with no shaping flag opens it.
func festivalsTUIRequested(cmd *cobra.Command, isTTY bool) bool {
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return false
	}
	if interactive, _ := cmd.Flags().GetBool("interactive"); interactive {
		return true
	}
	for _, f := range []string{"org", "tag", "status", "all", "all-campaigns", "since", "until", "sort"} {
		if cmd.Flags().Changed(f) {
			return false
		}
	}
	return isTTY
}

// Overridable in tests so the load can be exercised without the fest binary.
var festCLILookup = fest.FindFestCLI

func runFestivalsTUI(cmd *cobra.Command) error {
	ctx := cmd.Context()
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return runFestivalsText(cmd)
	}
	pathOutput, _ := cmd.Flags().GetString("path-output")

	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return camperrors.Wrap(err, "failed to load registry")
	}

	// Synchronous aggregate of the default active-campaign set (filters route to
	// the text path, so no passthrough here). A hard error returns BEFORE the
	// alt-screen opens, matching the non-TUI command's behavior.
	items, err := collectFestivalItems(ctx, selectCampaigns(reg, "", nil, false), nil, festCLILookup)
	if err != nil {
		return err
	}

	model := newFestivalsTUIModel(ctx, reg.FallbackOrg(), items)
	model.gotoEnabled = pathOutput != ""

	prog := tea.NewProgram(model, tea.WithContext(ctx), tea.WithAltScreen())
	final, err := prog.Run()
	if err != nil {
		return camperrors.Wrap(err, "running festivals browser")
	}
	return writeGotoSelection(final, pathOutput)
}

type festivalsTUIModel struct {
	ctx context.Context

	fallbackOrg string
	all         []festivalItem
	visible     []festivalItem
	cursor      int
	activeOnly  bool

	status    string
	statusErr bool

	gotoEnabled bool
	gotoPath    string

	width, height int
	quitting      bool
}

func newFestivalsTUIModel(ctx context.Context, fallbackOrg string, items []festivalItem) festivalsTUIModel {
	m := festivalsTUIModel{ctx: ctx, fallbackOrg: fallbackOrg}
	m.all = sortFestivals(items)
	m.rebuildVisible()
	return m
}

// sortFestivals orders rows so org/campaign headers stay contiguous, matching
// groupByOrgCampaign in the static renderer.
func sortFestivals(items []festivalItem) []festivalItem {
	out := make([]festivalItem, len(items))
	copy(out, items)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Org != b.Org {
			return a.Org < b.Org
		}
		if a.Campaign != b.Campaign {
			return a.Campaign < b.Campaign
		}
		return a.Festival < b.Festival
	})
	return out
}

func (m *festivalsTUIModel) rebuildVisible() {
	if !m.activeOnly {
		m.visible = m.all
	} else {
		out := make([]festivalItem, 0, len(m.all))
		for _, e := range m.all {
			if e.Status == "active" {
				out = append(out, e)
			}
		}
		m.visible = out
	}
	m.cursor = ui.ClampIdx(m.cursor, len(m.visible))
}

func (m festivalsTUIModel) Init() tea.Cmd { return nil }

// writeGotoSelection persists the festival the user chose, for the shell function
// to cd into. No-op unless a path-output file was supplied and a selection was
// made. The path is absolute, so the shell cd's to it directly.
func writeGotoSelection(final tea.Model, pathOutput string) error {
	m, ok := final.(festivalsTUIModel)
	if !ok || pathOutput == "" || m.gotoPath == "" {
		return nil
	}
	return os.WriteFile(pathOutput, []byte(m.gotoPath), 0o600)
}
