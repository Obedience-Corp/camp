//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Exec telemetry. The number that predicted every pool collapse so far,
// sustained exec latency against the Docker daemon, was never on screen
// during any incident. One line per run and one JSONL record keep the margin
// visible, so the next capacity regression surfaces as a trend instead of as
// six misattributed test failures. Best-effort by design: telemetry must
// never fail or slow a run.

var execStats struct {
	mu        sync.Mutex
	durations []time.Duration
}

// observeExecDuration records one container exec round trip. Called from the
// exec funnel for every command the suite runs.
func observeExecDuration(d time.Duration) {
	execStats.mu.Lock()
	execStats.durations = append(execStats.durations, d)
	execStats.mu.Unlock()
}

// execTelemetryLine renders the per-run capacity summary. Pure so it is
// testable without a run. The transport is part of the record: a p50 from
// session frames and a p50 from docker execs are different populations, and
// trend analysis must not mix them silently.
func execTelemetryLine(durations []time.Duration, transport string, poolSize int, wall time.Duration) string {
	if len(durations) == 0 {
		return fmt.Sprintf("harness telemetry: transport=%s execs=0 pool=%d wall=%s",
			transport, poolSize, wall.Round(time.Second))
	}
	sorted := slices.Clone(durations)
	slices.Sort(sorted)
	quantile := func(q float64) time.Duration {
		return sorted[int(q*float64(len(sorted)-1))]
	}
	return fmt.Sprintf("harness telemetry: transport=%s execs=%d p50=%s p95=%s max=%s pool=%d wall=%s",
		transport,
		len(sorted),
		quantile(0.50).Round(time.Millisecond),
		quantile(0.95).Round(time.Millisecond),
		sorted[len(sorted)-1].Round(time.Millisecond),
		poolSize,
		wall.Round(time.Second))
}

// reportExecTelemetry prints the capacity summary to stderr and appends a
// JSONL record under the repo's out/ directory. Called once from TestMain
// after m.Run.
func reportExecTelemetry(poolSize int, wall time.Duration, exitCode int) {
	execStats.mu.Lock()
	durations := slices.Clone(execStats.durations)
	execStats.mu.Unlock()

	fmt.Fprintln(os.Stderr, execTelemetryLine(durations, execTransportName(), poolSize, wall))
	appendTelemetryHistory(durations, poolSize, wall, exitCode)
}

func appendTelemetryHistory(durations []time.Duration, poolSize int, wall time.Duration, exitCode int) {
	sorted := slices.Clone(durations)
	slices.Sort(sorted)
	record := map[string]any{
		"ts":        time.Now().UTC().Format(time.RFC3339),
		"transport": execTransportName(),
		"execs":     len(sorted),
		"pool":      poolSize,
		"wall_s":    int(wall.Seconds()),
		"exit":      exitCode,
	}
	if len(sorted) > 0 {
		record["p50_ms"] = sorted[int(0.50*float64(len(sorted)-1))].Milliseconds()
		record["p95_ms"] = sorted[int(0.95*float64(len(sorted)-1))].Milliseconds()
		record["max_ms"] = sorted[len(sorted)-1].Milliseconds()
	}

	line, err := json.Marshal(record)
	if err != nil {
		return
	}

	// go test runs with the package directory as cwd, so the repo root is two
	// levels up. out/ is gitignored: the history is local capacity evidence,
	// not repository state.
	dir := filepath.Join("..", "..", "out")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "itest-history.jsonl"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}
