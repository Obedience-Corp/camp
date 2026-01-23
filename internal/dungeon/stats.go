package dungeon

import (
	"context"
	"encoding/json"
	"os/exec"
)

// StatsGatherer collects statistics about dungeon items.
type StatsGatherer struct {
	hasSCC  bool
	hasFest bool
}

// NewStatsGatherer creates a new stats gatherer, detecting available tools.
func NewStatsGatherer() *StatsGatherer {
	g := &StatsGatherer{}

	// Check for scc
	if _, err := exec.LookPath("scc"); err == nil {
		g.hasSCC = true
	}

	// Check for fest
	if _, err := exec.LookPath("fest"); err == nil {
		g.hasFest = true
	}

	return g
}

// Available returns true if at least one stats tool is available.
func (g *StatsGatherer) Available() bool {
	return g.hasSCC || g.hasFest
}

// Gather collects statistics for the given path.
// Returns nil if no stats tools are available.
func (g *StatsGatherer) Gather(ctx context.Context, path string) *ItemStats {
	if !g.Available() {
		return nil
	}

	// Try scc first (more detailed for code)
	if g.hasSCC {
		if stats := g.gatherSCC(ctx, path); stats != nil {
			return stats
		}
	}

	// Fall back to fest count
	if g.hasFest {
		if stats := g.gatherFest(ctx, path); stats != nil {
			return stats
		}
	}

	return nil
}

// sccOutput represents the JSON output from scc.
type sccOutput []struct {
	Name    string `json:"Name"`
	Files   int    `json:"Count"`
	Lines   int    `json:"Lines"`
	Code    int    `json:"Code"`
	Blank   int    `json:"Blank"`
	Comment int    `json:"Comment"`
}

// gatherSCC runs scc and parses its output.
func (g *StatsGatherer) gatherSCC(ctx context.Context, path string) *ItemStats {
	cmd := exec.CommandContext(ctx, "scc", "--format", "json", path)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var results sccOutput
	if err := json.Unmarshal(output, &results); err != nil {
		return nil
	}

	// Aggregate totals across all languages
	stats := &ItemStats{Source: "scc"}
	for _, lang := range results {
		stats.Files += lang.Files
		stats.Lines += lang.Lines
		stats.Code += lang.Code
	}

	return stats
}

// festCountOutput represents the JSON output from fest count.
type festCountOutput struct {
	Files  int `json:"files"`
	Lines  int `json:"lines"`
	Tokens int `json:"tokens"`
}

// gatherFest runs fest count and parses its output.
func (g *StatsGatherer) gatherFest(ctx context.Context, path string) *ItemStats {
	cmd := exec.CommandContext(ctx, "fest", "count", "--json", "--recursive", path)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var result festCountOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil
	}

	return &ItemStats{
		Files:  result.Files,
		Lines:  result.Lines,
		Tokens: result.Tokens,
		Source: "fest",
	}
}
